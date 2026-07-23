package device

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/pkg/logger"
)

var qmiControlStatFn = os.Stat
var qmiRecoveryControlStableInterval = 1200 * time.Millisecond

func workerATProbeOK(w *Worker, timeout time.Duration) bool {
	if w != nil {
		if resolvedBackendMode(w.ConfigSnapshot()) == backend.BackendQMI {
			return true
		}
		if w.Backend != nil && w.Backend.Mode() == backend.BackendQMI {
			return true
		}
	}
	if w == nil || w.Modem == nil || !w.Modem.HasATPort() {
		return true
	}
	if !w.Modem.CanExecuteAT() {
		return false
	}
	_, err := w.Modem.ExecuteATSilent("AT", timeout)
	return err == nil
}

// SendWorkerReboot submits a full modem reset through the active backend, with
// AT+CFUN fallback when the QMI control plane is unavailable.
func SendWorkerReboot(ctx context.Context, worker *Worker, forceATFirst bool) error {
	if worker == nil {
		return fmt.Errorf("设备未找到")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	useATFirst := forceATFirst || worker.Backend == nil || worker.Backend.Mode() != backend.BackendQMI
	tryAT := func() bool {
		if worker.Modem == nil || !worker.Modem.HasATPort() || !worker.Modem.CanExecuteAT() {
			return false
		}
		_, err := worker.Modem.ExecuteAT("AT+CFUN=1,1", 20*time.Second)
		if err == nil {
			return true
		}
		message := strings.ToLower(err.Error())
		return strings.Contains(message, "timeout") || strings.Contains(message, "eof") ||
			strings.Contains(message, "closed") || strings.Contains(message, "no such file")
	}

	rebootSent := false
	if useATFirst {
		rebootSent = tryAT()
	}
	var backendErr error
	if !rebootSent && worker.Backend != nil {
		if err := worker.Backend.Reboot(ctx); err != nil {
			backendErr = err
		} else {
			rebootSent = true
		}
	}
	if !rebootSent && !useATFirst {
		rebootSent = tryAT()
	}
	if rebootSent {
		return nil
	}
	if backendErr != nil {
		return fmt.Errorf("重启指令失败: %w", backendErr)
	}
	return fmt.Errorf("无法发送重启指令，无可用通道")
}

func (p *Pool) RebootWorkerAndRecover(ctx context.Context, worker *Worker, reason string) error {
	if p == nil || worker == nil || !p.isCurrentWorker(worker) {
		return fmt.Errorf("worker_unavailable")
	}
	if strings.TrimSpace(reason) == "" {
		reason = "modem_reboot"
	}
	// Claim recovery before sending the reset. Otherwise a udev event or health
	// check can win the race and a second caller may reset the same modem again.
	if !p.beginModemRebootRecovery(worker.ID) {
		return fmt.Errorf("modem_reboot_recovery_in_progress")
	}
	forceATFirst := worker.QMICore != nil && !worker.QMICore.IsControlReady()
	if err := SendWorkerReboot(ctx, worker, forceATFirst); err != nil {
		p.finishModemRebootRecovery(worker.ID)
		return err
	}
	p.MarkLifecycleRecovery(worker.ID, LifecyclePhaseRebooting, reason, 3*time.Minute)
	opts := defaultModemRebootRecoveryOptions(worker.ID, reason)
	opts.delays = commandedRebootRecoveryDelays(reason)
	go p.runModemRebootRecoveryWithClaim(opts, true)
	return nil
}

func (p *Pool) refreshModemRebootRecoveredIdentity(w *Worker, reason string) error {
	if w == nil {
		return fmt.Errorf("worker_nil")
	}
	if reason = strings.TrimSpace(reason); reason == "" {
		reason = "modem_reboot_recovery"
	}

	result, err := p.refreshIdentityAndApplyCardPolicy(w, reason)
	if err != nil {
		return fmt.Errorf("refresh_identity: %w", err)
	}

	if strings.TrimSpace(result.ICCID) == "" && strings.TrimSpace(result.IMSI) == "" {
		return fmt.Errorf("sim_identity_empty")
	}

	return nil
}

func (p *Pool) markQMIControlRecovered(worker *Worker, reason string) {
	if p == nil || worker == nil || !p.isCurrentWorker(worker) {
		return
	}
	if worker.QMICore != nil && !worker.QMICore.IsControlReady() {
		worker.setCachedHealthy(false)
		if p.lifecycle != nil {
			p.lifecycle.BeginRecovery(worker.ID, LifecyclePhaseRecovering, "qmi_control_not_ready", qmiLifecycleRecoveryTTL)
		}
		return
	}
	if reason = strings.TrimSpace(reason); reason == "" {
		reason = "qmi_control_recovered"
	}
	worker.RecordWatchdogEvent(WatchdogEvent{
		Layer:     HealthLayerQMI,
		State:     HealthStateHealthy,
		EventType: "qmi_control_recovered",
		Reason:    reason,
		At:        time.Now(),
	})
	worker.resetHealthFailureStreak()
	worker.setCachedHealthy(true)
	if p.lifecycle != nil {
		p.lifecycle.FinishOnline(worker.ID)
	}
}

type modemRebootRecoveryOptions struct {
	deviceID               string
	reason                 string
	delays                 []time.Duration
	removeBeforeScan       bool
	restoreVoWiFi          bool
	transportEvent         *TransportRecoveryEvent
	transportEventObserved bool
}

func defaultModemRebootRecoveryOptions(deviceID string, reason string) modemRebootRecoveryOptions {
	return modemRebootRecoveryOptions{
		deviceID:         deviceID,
		reason:           reason,
		delays:           []time.Duration{0, time.Second, 3 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second},
		removeBeforeScan: true,
		restoreVoWiFi:    true,
	}
}

// manualRebootRecoveryDelays 为手动重启专用扫描节奏：去掉 delay=0 的立即轮，
// 首轮等待模组真正复位后再扫描，避免命中尚未掉线的旧模组。
func manualRebootRecoveryDelays() []time.Duration {
	return []time.Duration{2 * time.Second, 3 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}
}

func commandedRebootRecoveryDelays(reason string) []time.Duration {
	switch strings.TrimSpace(reason) {
	case "manual_reboot", "network_enable_qmi_recovery", "esim_switch":
		return manualRebootRecoveryDelays()
	default:
		return defaultModemRebootRecoveryOptions("", "").delays
	}
}

func (p *Pool) beginModemRebootRecovery(deviceID string) bool {
	if p == nil || deviceID == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.modemRebootRecovering == nil {
		p.modemRebootRecovering = make(map[string]bool)
	}
	if p.modemRebootRecovering[deviceID] {
		return false
	}
	p.modemRebootRecovering[deviceID] = true
	if p.modemRebootWakeups == nil {
		p.modemRebootWakeups = make(map[string]chan struct{})
	}
	p.modemRebootWakeups[deviceID] = make(chan struct{}, 1)
	return true
}

func (p *Pool) finishModemRebootRecovery(deviceID string) {
	if p == nil || deviceID == "" {
		return
	}
	p.mu.Lock()
	delete(p.modemRebootRecovering, deviceID)
	delete(p.modemRebootWakeups, deviceID)
	p.mu.Unlock()
}

func (p *Pool) modemRebootWakeChannel(deviceID string) <-chan struct{} {
	if p == nil || deviceID == "" {
		return nil
	}
	p.mu.RLock()
	ch := p.modemRebootWakeups[deviceID]
	p.mu.RUnlock()
	return ch
}

func (p *Pool) waitModemRebootRecoveryTrigger(deviceID string, delay time.Duration) {
	ch := p.modemRebootWakeChannel(deviceID)
	if delay <= 0 {
		select {
		case <-ch:
		default:
		}
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ch:
	case <-p.ctx.Done():
	}
}

func (p *Pool) WakeModemRebootRecoveries(reason string) int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	woken := 0
	for deviceID, ch := range p.modemRebootWakeups {
		if !p.modemRebootRecovering[deviceID] || ch == nil {
			continue
		}
		select {
		case ch <- struct{}{}:
			woken++
		default:
		}
	}
	return woken
}

func modemRebootRecoveryConfig(deviceID string) (config.DeviceConfig, bool) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return config.DeviceConfig{}, false
	}
	if cfg, err := config.GetDeviceByID(deviceID); err == nil && cfg != nil {
		return *cfg, true
	}
	return config.DeviceConfig{}, false
}

func qmiControlPathStatOK(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := qmiControlStatFn(path)
	return err == nil
}

func qmiRecoveryControlPathStable(cfg config.DeviceConfig, interval time.Duration) bool {
	if !requiresQMICore(cfg) {
		return true
	}
	controlPath := strings.TrimSpace(cfg.ControlDevice)
	if controlPath == "" {
		return true
	}
	if !qmiControlPathStatOK(controlPath) {
		return false
	}
	if interval > 0 {
		time.Sleep(interval)
	}
	return qmiControlPathStatOK(controlPath)
}

type qmiRecoveryLiveCandidate struct {
	Device QMIDevice
	IMEI   string
}

type qmiRecoveryScanDecision struct {
	Ready      bool
	Reason     string
	Attachment QMIDevice
}

func qmiRecoveryScanGate(cfg config.DeviceConfig, live []qmiRecoveryLiveCandidate, discoveryAvailable bool) qmiRecoveryScanDecision {
	if !requiresQMICore(cfg) {
		return qmiRecoveryScanDecision{Ready: true, Reason: "non_qmi"}
	}
	configuredIMEI := strings.TrimSpace(cfg.ModemIMEI)
	if configuredIMEI != "" {
		for _, candidate := range live {
			if config.IMEIMatches(candidate.IMEI, configuredIMEI) {
				return qmiRecoveryScanDecision{
					Ready:      true,
					Reason:     "live_imei_match",
					Attachment: candidate.Device,
				}
			}
		}
	}
	if !discoveryAvailable {
		if qmiRecoveryControlPathStable(cfg, qmiRecoveryControlStableInterval) {
			return qmiRecoveryScanDecision{Ready: true, Reason: "configured_control_stable"}
		}
		return qmiRecoveryScanDecision{Ready: false, Reason: "configured_control_missing"}
	}
	for _, candidate := range live {
		if strings.TrimSpace(candidate.Device.ControlPath) == strings.TrimSpace(cfg.ControlDevice) ||
			strings.TrimSpace(candidate.Device.NetInterface) == strings.TrimSpace(cfg.Interface) ||
			strings.TrimSpace(candidate.Device.USBPath) == strings.TrimSpace(cfg.USBPath) {
			return qmiRecoveryScanDecision{
				Ready:      true,
				Reason:     "configured_attachment_seen",
				Attachment: candidate.Device,
			}
		}
	}
	return qmiRecoveryScanDecision{Ready: false, Reason: "no_matching_qmi_attachment"}
}

func (p *Pool) qmiRecoveryLiveCandidates(cfg config.DeviceConfig) ([]qmiRecoveryLiveCandidate, bool) {
	discovered, err := discoverQMIDevicesFn()
	if err != nil {
		logger.Warn("模组重启恢复：QMI attachment 扫描失败，将回退到配置控制口检查",
			"device", strings.TrimSpace(cfg.ID),
			"err", err)
		return nil, false
	}
	candidates := make([]qmiRecoveryLiveCandidate, 0, len(discovered))
	liveWorkerIndex := BuildWorkerDiscoveryIndex(p.GetAllWorkers(), false)
	for _, raw := range discovered {
		dev := raw
		imei := ""
		if liveInfo, ok := liveWorkerIndex.Lookup(raw.ControlPath, raw.USBPath, raw.NetInterface); ok {
			if liveInfo.IMEI != "" {
				imei = liveInfo.IMEI
			}
			if containsPort(raw.ATPorts, liveInfo.ATPort) {
				dev.ATPort = liveInfo.ATPort
			}
		}
		if imei == "" {
			dev, imei = resolveDiscoveredQMIDeviceFn(raw, 1600*time.Millisecond, true)
		}
		candidates = append(candidates, qmiRecoveryLiveCandidate{
			Device: dev,
			IMEI:   imei,
		})
	}
	return candidates, true
}

func qmiStartCoreFailureShouldAbortWorker(message string) bool {
	return qmiErrorIndicatesTransportDown(message)
}

func modemRebootRecoveryShouldRebuildAfterReadinessFailure(opts modemRebootRecoveryOptions, err error) bool {
	if err == nil {
		return false
	}
	reason := strings.TrimSpace(opts.reason)
	if reason != "manual_reboot" && !opts.removeBeforeScan {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"live_identity_empty",
		"sim_identity_empty",
		"refresh_identity:",
		"refresh_runtime:",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func qmiWorkerControlReady(worker *Worker) bool {
	if worker == nil {
		return false
	}
	if worker.QMICore != nil {
		return worker.QMICore.IsControlReady()
	}
	snapshot := worker.HealthSnapshot()
	return snapshot.State == HealthStateHealthy && snapshot.Layer == HealthLayerQMI
}

func modemRebootRecoveryShouldRebuildAfterReadinessFailureForWorker(opts modemRebootRecoveryOptions, worker *Worker, err error) bool {
	if err == nil {
		return false
	}
	if qmiWorkerControlReady(worker) {
		message := strings.ToLower(err.Error())
		for _, fragment := range []string{
			"refresh_identity:",
			"sim_identity_empty",
			"identity not readable",
			"live_identity_empty",
		} {
			if strings.Contains(message, fragment) {
				return false
			}
		}
	}
	return modemRebootRecoveryShouldRebuildAfterReadinessFailure(opts, err)
}

// modemRebootRecoveryShouldRebuildAfterTransportDown 判断身份刷新失败是否源于
// 控制面传输已真正断开。仅当 Worker 当前控制面不健康且错误为传输断开时返回 true。
func modemRebootRecoveryShouldRebuildAfterTransportDown(worker *Worker, err error) bool {
	if err == nil {
		return false
	}
	if qmiWorkerControlReady(worker) {
		return false
	}
	return qmiErrorIndicatesTransportDown(err.Error())
}

func (p *Pool) ScheduleModemRebootRecovery(deviceID string, reason string) {
	opts := defaultModemRebootRecoveryOptions(deviceID, reason)
	opts.delays = commandedRebootRecoveryDelays(reason)
	if !p.beginModemRebootRecovery(deviceID) {
		logger.Debug("模组重启恢复已在运行，跳过重复调度", "device", deviceID, "reason", reason)
		return
	}
	go p.runModemRebootRecoveryWithClaim(opts, true)
}

// ScheduleNetworkControlRecovery resets the modem through the auxiliary AT
// path before rebuilding the worker. A worker-only rebuild cannot recover a
// cdc-wdm/QMI control plane that remains wedged across reopen attempts.
func (p *Pool) ScheduleNetworkControlRecovery(worker *Worker, reason string) {
	p.scheduleNetworkControlRecoveryWithEvent(worker, reason, nil)
}

func (p *Pool) scheduleNetworkControlRecoveryWithEvent(worker *Worker, reason string, event *TransportRecoveryEvent) bool {
	if p == nil || worker == nil || strings.TrimSpace(worker.ID) == "" {
		return false
	}
	if !p.isCurrentWorker(worker) {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "network_control_recovery"
	}

	eventObserved := false
	if event != nil && p.transportRecovery != nil {
		if event.DeviceID == "" {
			event.DeviceID = worker.ID
		}
		if event.WorkerGeneration == 0 {
			event.WorkerGeneration = worker.generation
		}
		accepted, overLimit := p.transportRecovery.ObserveWithBudget(*event)
		if !accepted {
			if overLimit {
				worker.RecordWatchdogEvent(WatchdogEvent{
					Layer:     HealthLayerPool,
					State:     HealthStateFailed,
					EventType: "transport_recovery_giveup",
					Reason:    reason,
					Err:       event.Err,
				})
				if p.lifecycle != nil {
					p.lifecycle.SetPhase(worker.ID, LifecyclePhaseDegraded, "transport_recovery_giveup", 0)
				}
				logger.Warn("QMI 启动复位超过滑窗上限，停止自动复位",
					"device", worker.ID, "reason", reason, "err", event.Err)
			} else {
				logger.Debug("QMI 控制面恢复已在运行，跳过重复复位", "device", worker.ID, "reason", reason)
			}
			return false
		}
		eventObserved = true
	}
	if !p.beginModemRebootRecovery(worker.ID) {
		if eventObserved && p.transportRecovery != nil {
			p.transportRecovery.Finish(worker.ID)
		}
		logger.Debug("网络控制面恢复已在运行，跳过重复复位", "device", worker.ID, "reason", reason)
		return false
	}
	opts := defaultModemRebootRecoveryOptions(worker.ID, reason)
	opts.delays = manualRebootRecoveryDelays()
	opts.transportEvent = event
	opts.transportEventObserved = eventObserved
	go func() {
		if err := resetWorkerForNetworkControlRecovery(worker); err != nil {
			logger.Warn("网络控制面恢复发送模组复位失败，继续尝试重新接管", "device", worker.ID, "reason", reason, "err", err)
		}
		p.runModemRebootRecoveryWithClaim(opts, true)
	}()
	return true
}

func resetWorkerForNetworkControlRecovery(worker *Worker) error {
	if worker == nil {
		return fmt.Errorf("worker 不存在")
	}
	if err := resetWorkerViaAuxiliaryAT(worker, 20*time.Second); err == nil || modemResetCommandLikelyAccepted(err) {
		return nil
	}
	if worker.Backend == nil {
		return fmt.Errorf("无可用模组复位通道")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return worker.Backend.Reboot(ctx)
}

// resetQMIWorkersForProcessShutdown gives converted DJI firmware a fresh QMI
// control plane for the next process. Closing and reopening cdc-wdm alone
// leaves these modules unable to allocate any service client IDs.
func resetQMIWorkersForProcessShutdown(workers []*Worker) {
	var wg sync.WaitGroup
	for _, worker := range workers {
		if worker == nil || !requiresQMICore(worker.ConfigSnapshot()) || worker.Modem == nil || !worker.Modem.HasATPort() {
			continue
		}
		wg.Add(1)
		go func(worker *Worker) {
			defer wg.Done()
			err := resetWorkerViaAuxiliaryAT(worker, 5*time.Second)
			if err == nil || modemResetCommandLikelyAccepted(err) {
				return
			}
			logger.Warn("进程关闭前复位 QMI 模组失败", "device", worker.ID, "err", err)
		}(worker)
	}
	wg.Wait()
}

func resetWorkerViaAuxiliaryAT(worker *Worker, timeout time.Duration) error {
	if worker == nil || worker.Modem == nil || !worker.Modem.HasATPort() {
		return fmt.Errorf("辅助 AT 口不可用")
	}
	if worker.Modem.CanExecuteAT() {
		_, err := worker.Modem.ExecuteAT("AT+CFUN=1,1", timeout)
		return err
	}

	serialAT, err := modem.NewSerialAT(worker.Modem.ATPort(), 115200, 8, 1, "N")
	if err != nil {
		return fmt.Errorf("打开辅助 AT 口失败: %w", err)
	}
	defer serialAT.Close()
	response, err := serialAT.Execute("AT+CFUN=1,1", timeout)
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToUpper(response), "ERROR") {
		return fmt.Errorf("模组拒绝复位命令: %s", strings.TrimSpace(response))
	}
	return nil
}

func modemResetCommandLikelyAccepted(err error) bool {
	if err == nil {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{"timeout", "eof", "closed", "no such file", "no such device", "input/output error"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func (p *Pool) scheduleWorkerRecoveryWithTransportEvent(deviceID string, reason string, event *TransportRecoveryEvent) bool {
	deviceID = strings.TrimSpace(deviceID)
	reason = strings.TrimSpace(reason)
	if p == nil || deviceID == "" {
		return false
	}
	if reason == "" {
		reason = "worker_recovery"
	}
	opts := defaultModemRebootRecoveryOptions(deviceID, reason)
	opts.transportEvent = event
	if event != nil && p.transportRecovery != nil {
		if event.DeviceID == "" {
			event.DeviceID = deviceID
		}
		accepted, overLimit := p.transportRecovery.ObserveWithBudget(*event)
		if !accepted {
			if overLimit {
				if worker := p.GetWorker(deviceID); worker != nil {
					worker.RecordWatchdogEvent(WatchdogEvent{
						Layer:     HealthLayerPool,
						State:     HealthStateFailed,
						EventType: "transport_recovery_giveup",
						Reason:    reason,
						Err:       event.Err,
					})
				}
				if p.lifecycle != nil {
					p.lifecycle.SetPhase(deviceID, LifecyclePhaseDegraded, "transport_recovery_giveup", 0)
				}
				logger.Warn("传输恢复重建超过滑窗上限，停止自动重建",
					"device", deviceID, "reason", reason, "err", event.Err)
				return false
			}
			logger.Debug("QMI 恢复已在运行，跳过重复调度", "device", deviceID, "reason", reason)
			return false
		}
		opts.transportEventObserved = true
	}
	if worker := p.GetWorker(deviceID); worker != nil {
		worker.RecordWatchdogEvent(WatchdogEvent{
			Layer:     HealthLayerPool,
			State:     HealthStateReprobing,
			EventType: "worker_reprobe",
			Reason:    reason,
		})
	}
	go p.runModemRebootRecovery(opts)
	return true
}

func (p *Pool) scheduleWorkerRecovery(deviceID string, reason string) {
	deviceID = strings.TrimSpace(deviceID)
	reason = strings.TrimSpace(reason)
	if p == nil || deviceID == "" {
		return
	}
	if reason == "" {
		reason = "worker_recovery"
	}
	if worker := p.GetWorker(deviceID); worker != nil {
		worker.RecordWatchdogEvent(WatchdogEvent{
			Layer:     HealthLayerPool,
			State:     HealthStateReprobing,
			EventType: "worker_reprobe",
			Reason:    reason,
		})
	}
	p.ScheduleModemRebootRecovery(deviceID, reason)
}

func (p *Pool) scheduleATDisconnectRecovery(deviceID string, reason string) {
	deviceID = strings.TrimSpace(deviceID)
	reason = strings.TrimSpace(reason)
	if p == nil || deviceID == "" {
		return
	}
	if reason == "" {
		reason = "modem_disconnect"
	}
	if worker := p.GetWorker(deviceID); worker != nil {
		state := HealthStateRecovering
		if reason == "at_timeout_threshold" {
			state = HealthStateInvalid
		}
		worker.RecordWatchdogEvent(WatchdogEvent{
			Layer:     HealthLayerAT,
			State:     state,
			EventType: reason,
			Reason:    reason,
			Threshold: func() int {
				if reason == "at_timeout_threshold" {
					return 5
				}
				return 0
			}(),
		})
		worker.RecordWatchdogEvent(WatchdogEvent{
			Layer:     HealthLayerPool,
			State:     HealthStateReprobing,
			EventType: "worker_reprobe",
			Reason:    reason,
		})
	}
	opts := defaultModemRebootRecoveryOptions(deviceID, reason)
	opts.removeBeforeScan = false
	go p.runModemRebootRecovery(opts)
}

func (p *Pool) runModemRebootRecovery(opts modemRebootRecoveryOptions) {
	p.runModemRebootRecoveryWithClaim(opts, false)
}

func (p *Pool) runModemRebootRecoveryWithClaim(opts modemRebootRecoveryOptions, preclaimed bool) {
	if p == nil || opts.deviceID == "" {
		return
	}
	if !preclaimed && !p.beginModemRebootRecovery(opts.deviceID) {
		if opts.transportEventObserved && p.transportRecovery != nil {
			p.transportRecovery.Finish(opts.deviceID)
		}
		logger.Debug("模组重启恢复已在运行，跳过重复调度", "device", opts.deviceID, "reason", opts.reason)
		return
	}

	if opts.transportEvent != nil && p.transportRecovery != nil && !opts.transportEventObserved {
		if !p.transportRecovery.Observe(*opts.transportEvent) {
			p.finishModemRebootRecovery(opts.deviceID)
			logger.Debug("QMI 恢复已在运行，释放 modem reboot 锁并跳过", "device", opts.deviceID, "reason", opts.reason)
			return
		}
	}

	defer func() {
		p.finishModemRebootRecovery(opts.deviceID)
		if p.transportRecovery != nil {
			p.transportRecovery.Finish(opts.deviceID)
		}
	}()
	initialControlReady := false
	if worker := p.GetWorker(opts.deviceID); worker != nil {
		initialControlReady = qmiWorkerControlReady(worker)
		worker.RecordWatchdogEvent(WatchdogEvent{
			Layer:     HealthLayerPool,
			State:     HealthStateReprobing,
			EventType: "modem_reboot_recovery_start",
			Reason:    opts.reason,
		})
	}

	hadVoWiFi := false
	if opts.restoreVoWiFi {
		hadVoWiFi = p.teardownVoWiFiForReconnect(opts.deviceID)
	}
	if p.lifecycle != nil {
		p.lifecycle.BeginRecovery(opts.deviceID, LifecyclePhaseUSBWait, opts.reason, qmiLifecycleRecoveryTTL)
	}
	if opts.removeBeforeScan {
		if err := p.RemoveWorker(opts.deviceID); err != nil {
			logger.Debug("模组重启恢复：旧 Worker 已不存在", "device", opts.deviceID, "err", err)
		}
	}
	cfg, hasCfg := modemRebootRecoveryConfig(opts.deviceID)

	for round, delay := range opts.delays {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		p.waitModemRebootRecoveryTrigger(opts.deviceID, delay)
		if p.ctx.Err() != nil {
			return
		}
		if hasCfg && requiresQMICore(cfg) {
			decision := p.ResolveQMIRecoveryAttachment(cfg)
			if !decision.Ready {
				logger.Debug("模组重启恢复：QMI attachment 尚未可用，继续等待",
					"device", opts.deviceID,
					"round", round+1,
					"reason", decision.Reason,
					"control", strings.TrimSpace(cfg.ControlDevice))
				continue
			}
			logger.Debug("模组重启恢复：QMI attachment 可用于扫描",
				"device", opts.deviceID,
				"round", round+1,
				"reason", decision.Reason,
				"control", strings.TrimSpace(decision.Attachment.ControlPath),
				"interface", strings.TrimSpace(decision.Attachment.NetInterface))
		}
		logger.Info(fmt.Sprintf("[%s] 模组重启恢复扫描 (第 %d/%d 轮)", opts.deviceID, round+1, len(opts.delays)))
		var err error
		if p.rescanAndReconnectForTest != nil {
			err = p.rescanAndReconnectForTest()
		} else {
			err = p.rescanAndReconnect(rescanReconnectOptions{
				targetDeviceID: opts.deviceID,
			})
		}
		if err != nil {
			logger.Warn("模组重启恢复扫描失败", "device", opts.deviceID, "round", round+1, "err", err)
			continue
		}
		worker := p.GetWorker(opts.deviceID)
		if worker != nil {
			controlReadyBeforeIdentityRefresh := qmiWorkerControlReady(worker) || initialControlReady
			if err := p.refreshModemRebootRecoveredIdentity(worker, opts.reason); err != nil {
				logger.Warn("模组重启恢复后 SIM 身份未就绪，继续等待",
					"device", opts.deviceID,
					"round", round+1,
					"err", err)
				if modemRebootRecoveryShouldRebuildAfterTransportDown(worker, err) {
					logger.Warn("模组重启恢复检测到控制面传输已断开，移除 Worker 等待下一轮重新接管",
						"device", opts.deviceID,
						"round", round+1,
						"reason", opts.reason,
						"err", err)
					if removeErr := p.RemoveWorker(opts.deviceID); removeErr != nil {
						logger.Warn("模组重启恢复移除传输断开 Worker 失败",
							"device", opts.deviceID,
							"round", round+1,
							"err", removeErr)
					}
				} else if modemRebootRecoveryShouldRebuildAfterReadinessFailureForWorker(opts, worker, err) {
					logger.Warn("模组重启恢复检测到半就绪 Worker，移除后等待下一轮重新接管",
						"device", opts.deviceID,
						"round", round+1,
						"reason", opts.reason,
						"err", err)
					if removeErr := p.RemoveWorker(opts.deviceID); removeErr != nil {
						logger.Warn("模组重启恢复移除半就绪 Worker 失败",
							"device", opts.deviceID,
							"round", round+1,
							"err", removeErr)
					}
				} else if controlReadyBeforeIdentityRefresh {
					logger.Info("模组重启恢复：QMI 控制面已恢复，SIM 身份转入后台收敛",
						"device", opts.deviceID,
						"round", round+1,
						"reason", opts.reason,
						"err", err)
					p.startQMIIdentityConvergence(worker, opts.reason)
					return
				}
				continue
			}
			if requiresQMICore(worker.ConfigSnapshot()) && !qmiWorkerControlReady(worker) {
				logger.Info("模组重启恢复：SIM 身份已恢复，等待 QMI 控制面就绪",
					"device", opts.deviceID,
					"round", round+1,
					"reason", opts.reason)
				continue
			}
			logger.Info("模组重启恢复成功", "device", opts.deviceID, "round", round+1)
			p.markQMIControlRecovered(worker, opts.reason)
			if hadVoWiFi {
				go func(deviceID string) {
					if err := p.enableVoWiFiWhenReady(deviceID, 5*time.Second, opts.reason); err != nil {
						logger.Warn("模组重启恢复后恢复 VoWiFi 失败", "device", deviceID, "err", err)
					}
				}(opts.deviceID)
			}
			return
		}
	}
	if worker := p.GetWorker(opts.deviceID); worker != nil {
		worker.RecordWatchdogEvent(WatchdogEvent{
			Layer:     HealthLayerPool,
			State:     HealthStateFailed,
			EventType: "modem_reboot_recovery_exhausted",
			Reason:    opts.reason,
		})
	}
	logger.Warn("模组重启恢复多轮扫描未恢复，等待健康检查兜底", "device", opts.deviceID, "reason", opts.reason)
}
