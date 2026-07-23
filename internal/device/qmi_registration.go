package device

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	qmimanager "github.com/iniwex5/quectel-qmi-go/pkg/manager"
	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	qmipkg "github.com/iniwex5/vohive/internal/qmi"
	"github.com/iniwex5/vohive/pkg/logger"
)

var (
	errQMIRegistrationDenied  = errors.New("qmi_registration_denied")
	errQMIRegistrationSkipped = errors.New("qmi_registration_skipped")
	errQMISIMNotReady         = errors.New("qmi_sim_not_ready")
)

const (
	qmiRegistrationForceSearchAfterTries                = 2
	qmiRegistrationUnregisteredRadioCycleAfterTries     = 30
	qmiRegistrationDefaultMaxAttempts                   = 180
	qmiRegistrationUnsupportedForceRadioCycleAfterTries = 3

	// qmiRegistrationTimeoutDataRequired 用于数据网络必须就绪的协调路径（如 StartNetwork），
	// DMS/NAS 偶发卡顿及漫游选网较慢时仍要尽量等到驻网完成。
	qmiRegistrationTimeoutDataRequired = 390 * time.Second
	// qmiRegistrationTimeoutBestEffort 用于网络未开启时的驻网保活。该窗口必须覆盖
	// radio cycle 前的搜网和 cycle 后的完整宽限期，避免恢复动作刚完成就因超时退出。
	qmiRegistrationTimeoutBestEffort = 375 * time.Second
)

func qmiRegistrationTimeout(requiredForData bool) time.Duration {
	if requiredForData {
		return qmiRegistrationTimeoutDataRequired
	}
	return qmiRegistrationTimeoutBestEffort
}

type qmiSIMStatusSource interface {
	GetSIMStatus(ctx context.Context) (qmi.SIMStatus, error)
}

type qmiProvisioningEnsurer interface {
	EnsureSIMProvisioned(ctx context.Context, opts qmimanager.EnsureSIMProvisionedOptions) (qmimanager.UIMReadiness, error)
}

// 编译期保证 *qmipkg.Manager 满足 ensurer 接口；签名漂移将直接 break build 而非静默跳过。
var _ qmiProvisioningEnsurer = (*qmipkg.Manager)(nil)

type qmiRegistrationController interface {
	GetServingSystem(ctx context.Context) (*backend.ServingSystem, error)
	NASInitiateNetworkRegister(ctx context.Context, req backend.NASRegisterRequest) error
	NASForceNetworkSearch(ctx context.Context) error
	NASSetSystemSelectionAutomatic(ctx context.Context) error
	NASAttachDetach(ctx context.Context, attached bool) error
	GetOperatingMode(ctx context.Context) (backend.OperatingMode, error)
	SetOperatingMode(ctx context.Context, mode backend.OperatingMode) error
}

type qmiRegistrationOptions struct {
	PollInterval       time.Duration
	MaxAttempts        int
	RegistrationOnly   bool
	ATRegistration     func() (int, string, bool)
	SuppressRadioCycle func() bool
}

func normalizeQMIRegistrationOptions(opts qmiRegistrationOptions) qmiRegistrationOptions {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = qmiRegistrationDefaultMaxAttempts
	}
	return opts
}

func ensureQMIRegistration(ctx context.Context, deviceID string, cfg config.DeviceConfig, sim qmiSIMStatusSource, ctrl qmiRegistrationController, opts qmiRegistrationOptions) error {
	if sim == nil {
		return fmt.Errorf("qmi sim source unavailable")
	}
	if ctrl == nil {
		return fmt.Errorf("qmi registration controller unavailable")
	}
	opts = normalizeQMIRegistrationOptions(opts)
	startedAt := time.Now()

	mode, err := ctrl.GetOperatingMode(ctx)
	if err != nil {
		return fmt.Errorf("读取 QMI radio mode 失败: %w", err)
	}
	logger.Debug("QMI radio mode 初始检查", "device", deviceID, "mode", int(mode))
	radioRestoredOnline := false
	if isFlightOperatingMode(mode) {
		logger.Info("QMI radio 初始处于飞行/低功耗，恢复 Online 后再驻网", "device", deviceID, "mode", int(mode))
		if err := ctrl.SetOperatingMode(ctx, backend.ModeOnline); err != nil {
			return fmt.Errorf("QMI radio mode 恢复 Online 失败: %w", err)
		}
		radioRestoredOnline = true
		if err := sleepQMIRegistrationPoll(ctx, opts.PollInterval); err != nil {
			return err
		}
		mode, err = ctrl.GetOperatingMode(ctx)
		if err != nil {
			return fmt.Errorf("恢复 Online 后读取 QMI radio mode 失败: %w", err)
		}
		logger.Debug("QMI radio mode 恢复后复查", "device", deviceID, "mode", int(mode))
		if isFlightOperatingMode(mode) {
			return fmt.Errorf("QMI radio mode 仍处于飞行/低功耗，无法驻网: mode=%d", int(mode))
		}
	}

	if ensurer, ok := sim.(qmiProvisioningEnsurer); ok {
		if _, perr := ensurer.EnsureSIMProvisioned(ctx, qmimanager.EnsureSIMProvisionedOptions{}); perr != nil {
			logger.Debug("QMI provisioning 收敛 best-effort 失败，继续等待 SIM ready", "device", deviceID, "err", perr)
		}
	}

	if err := waitQMISIMReady(ctx, deviceID, sim, opts); err != nil {
		return err
	}

	registerIssued := false
	attachIssued := false
	forceNetworkSearchIssued := false
	forceNetworkSearchUnsupported := false
	radioCycleIssued := false
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		ss, err := ctrl.GetServingSystem(ctx)
		if err != nil {
			return fmt.Errorf("读取 QMI serving system 失败: %w", err)
		}
		if ss == nil {
			return fmt.Errorf("读取 QMI serving system 返回空结果")
		}
		if ss.RegStatus != 1 && ss.RegStatus != 5 && opts.ATRegistration != nil && attempt%5 == 0 {
			if atStatus, atText, ok := opts.ATRegistration(); ok {
				switch atStatus {
				case 1, 5:
					if opts.RegistrationOnly {
						logger.Info("QMI 与 AT 驻网状态不一致，采用 AT 已注册状态结束协调",
							"device", deviceID, "qmi_reg_status", ss.RegStatus, "at_reg_status", atStatus)
						return nil
					}
				case 3:
					return fmt.Errorf("%w: %s", errQMIRegistrationDenied, atText)
				}
			}
		}

		needsRegistrationRecovery := false
		switch ss.RegStatus {
		case 1, 5:
			if ss.PSAttached || opts.RegistrationOnly {
				logger.Debug("QMI 驻网协调完成", "device", deviceID, "attempt", attempt, "elapsed_ms", time.Since(startedAt).Milliseconds(), "reg_status", ss.RegStatus, "radio_cycle_used", radioCycleIssued, "force_network_search_unsupported", forceNetworkSearchUnsupported)
				return nil
			}
			if !attachIssued {
				logger.Info("QMI 已驻网但未 PS attach，发起 NAS attach", "device", deviceID, "reg_status", ss.RegStatus)
				if err := ctrl.NASAttachDetach(ctx, true); err != nil {
					return fmt.Errorf("QMI PS attach 失败: %w", err)
				}
				attachIssued = true
			}
		case 2:
			// Searching is an active modem state. Reissuing registration, force-search
			// or an RF cycle restarts PLMN selection and can indefinitely delay roaming.
			// Leave the modem alone until it registers or reports a terminal state.
			if cfg.OperatorSelectionMode == "manual" && strings.TrimSpace(cfg.OperatorSelectionPLMN) != "" {
				needsRegistrationRecovery = true
				if !registerIssued {
					if err := initiateQMIRegistration(ctx, deviceID, cfg, ctrl); err != nil {
						return fmt.Errorf("QMI NAS 手动注册失败: %w", err)
					}
					registerIssued = true
				}
			}
		case 3:
			return fmt.Errorf("%w: %s", errQMIRegistrationDenied, ss.RegStatusText)
		default:
			needsRegistrationRecovery = true
			if !registerIssued {
				logger.Info("QMI 未驻网，发起 NAS 注册", "device", deviceID, "reg_status", ss.RegStatus)
				if err := initiateQMIRegistration(ctx, deviceID, cfg, ctrl); err != nil {
					return fmt.Errorf("QMI NAS 注册失败: %w", err)
				}
				registerIssued = true
			}
		}

		// 仅明确未注册或手动选网时进入恢复链路。自动选网的搜索中状态保持被动，
		// 避免重复命令重启模组正在进行的 PLMN 搜索。
		if needsRegistrationRecovery {
			if shouldForceNetworkSearchForQMIRegistration(attempt, registerIssued, forceNetworkSearchIssued, forceNetworkSearchUnsupported) {
				forceNetworkSearchIssued = true
				logger.Info("QMI 驻网持续未恢复，执行 NAS force network search", "device", deviceID, "attempt", attempt, "reg_status", ss.RegStatus)
				if err := ctrl.NASForceNetworkSearch(ctx); err != nil {
					if isUnsupportedQMIForceNetworkSearchError(err) {
						forceNetworkSearchUnsupported = true
						logger.Info("QMI NAS force network search 不受设备支持，后续跳过并提前执行 radio cycle", "device", deviceID, "err", err)
					} else {
						logger.Warn("QMI NAS force network search 失败，继续等待模组自主驻网", "device", deviceID, "err", err)
					}
				}
			}
			if shouldRadioCycleForQMIRegistration(attempt, ss.RegStatus, registerIssued, radioCycleIssued, forceNetworkSearchUnsupported, radioRestoredOnline) {
				if opts.SuppressRadioCycle != nil && opts.SuppressRadioCycle() {
					logger.Info("QMI 驻网恢复暂缓 radio cycle：运营商扫描进行中", "device", deviceID, "attempt", attempt)
				} else {
					radioCycleIssued = true
					if err := radioCycleQMIForRegistration(ctx, deviceID, ctrl, opts.PollInterval); err != nil {
						logger.Warn("QMI 驻网恢复 radio cycle 失败，继续等待模组自主驻网", "device", deviceID, "err", err)
					} else {
						registerIssued = false
						attachIssued = false
					}
				}
			}
		}

		if err := sleepQMIRegistrationPoll(ctx, opts.PollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("QMI 驻网/PS attach 超时: attempts=%d", opts.MaxAttempts)
}

func initiateQMIRegistration(ctx context.Context, deviceID string, cfg config.DeviceConfig, ctrl qmiRegistrationController) error {
	if cfg.OperatorSelectionMode == "manual" && cfg.OperatorSelectionPLMN != "" {
		sel, err := backend.NormalizeManualOperatorSelection(
			cfg.OperatorSelectionPLMN,
			backend.OperatorAccessTechnology(cfg.OperatorSelectionRAT),
			nil,
		)
		if err != nil {
			logger.Warn("QMI 手动驻网配置的 PLMN 不合法，降级为自动驻网", "device", deviceID, "plmn", cfg.OperatorSelectionPLMN, "err", err)
			return initiateQMIAutomaticRegistration(ctx, deviceID, ctrl)
		}

		req, err := backend.BuildManualNASRegisterRequest(sel)
		if err != nil {
			return fmt.Errorf("QMI NAS 手动注册参数无效: %w", err)
		}
		err = ctrl.NASInitiateNetworkRegister(ctx, req)
		if err != nil {
			return fmt.Errorf("QMI NAS 手动注册失败: %w", err)
		}
		logger.Info("QMI NAS 手动注册已提交", "device", deviceID, "plmn", cfg.OperatorSelectionPLMN)
		return nil
	}
	return initiateQMIAutomaticRegistration(ctx, deviceID, ctrl)
}

func initiateQMIAutomaticRegistration(ctx context.Context, deviceID string, ctrl qmiRegistrationController) error {
	selectionErr := ctrl.NASSetSystemSelectionAutomatic(ctx)
	if selectionErr != nil {
		logger.Warn("QMI 系统选择自动模式提交失败，继续尝试 NAS 自动注册", "device", deviceID, "err", selectionErr)
	} else {
		logger.Debug("QMI 系统选择自动模式已提交", "device", deviceID)
	}

	err := ctrl.NASInitiateNetworkRegister(ctx, backend.NASRegisterRequest{
		Mode:              "automatic",
		ChangeDuration:    qmi.NASChangeDurationPermanent,
		HasChangeDuration: true,
	})
	if err == nil {
		return nil
	}
	if !shouldFallbackQMIAutomaticRegistration(err) {
		return err
	}
	if selectionErr == nil {
		logger.Warn("QMI NAS 自动注册命令不兼容，已保留系统选择自动模式", "device", deviceID, "err", err)
		return nil
	}
	logger.Warn("QMI NAS 自动注册命令不兼容，改用系统选择自动模式", "device", deviceID, "err", err)
	if fallbackErr := ctrl.NASSetSystemSelectionAutomatic(ctx); fallbackErr != nil {
		logger.Warn("QMI 系统选择自动模式 fallback 失败，继续等待模组自主驻网", "device", deviceID, "err", fallbackErr)
		return nil
	}
	logger.Info("QMI 系统选择自动模式 fallback 已提交", "device", deviceID)
	return nil
}

func shouldForceNetworkSearchForQMIRegistration(attempt int, registerIssued bool, forceNetworkSearchIssued bool, forceNetworkSearchUnsupported bool) bool {
	return registerIssued && !forceNetworkSearchIssued && !forceNetworkSearchUnsupported && attempt >= qmiRegistrationForceSearchAfterTries
}

func shouldRadioCycleForQMIRegistration(attempt int, regStatus int, registerIssued bool, radioCycleIssued bool, forceNetworkSearchUnsupported bool, radioRestoredOnline bool) bool {
	if !registerIssued || radioCycleIssued || regStatus == 2 {
		return false
	}
	if forceNetworkSearchUnsupported && !radioRestoredOnline {
		return attempt >= qmiRegistrationUnsupportedForceRadioCycleAfterTries
	}
	return attempt >= qmiRegistrationUnregisteredRadioCycleAfterTries
}

func radioCycleQMIForRegistration(ctx context.Context, deviceID string, ctrl qmiRegistrationController, wait time.Duration) error {
	if ctrl == nil {
		return fmt.Errorf("qmi registration controller unavailable")
	}
	if wait <= 0 {
		wait = 2 * time.Second
	}
	logger.Info("QMI 搜网持续未恢复，执行 radio flight-mode cycle 重新触发搜网", "device", deviceID)

	if err := ctrl.SetOperatingMode(ctx, backend.ModeRFOff); err != nil {
		return fmt.Errorf("设置 RFOff 失败: %w", err)
	}
	if err := sleepQMIRegistrationPoll(ctx, wait); err != nil {
		return err
	}
	if err := ctrl.SetOperatingMode(ctx, backend.ModeOnline); err != nil {
		return fmt.Errorf("恢复 Online 失败: %w", err)
	}
	if err := sleepQMIRegistrationPoll(ctx, wait); err != nil {
		return err
	}
	return nil
}

func shouldFallbackQMIAutomaticRegistration(err error) bool {
	var qmiErr *qmi.QMIError
	if !errors.As(err, &qmiErr) {
		return false
	}
	return qmiErr.Service == 0x03 &&
		qmiErr.MessageID == qmi.NASInitiateNetworkRegister &&
		(qmiErr.ErrorCode == qmi.QMIErrMalformedMsg ||
			qmiErr.ErrorCode == qmi.QMIErrInvalidRegisterAction ||
			qmiErr.ErrorCode == qmi.QMIErrNoEffect ||
			qmiErr.ErrorCode == qmi.QMIErrNotSupported ||
			qmiErr.ErrorCode == qmi.QMIErrInvalidQmiCmd ||
			qmiErr.ErrorCode == qmi.QMIErrOpDeviceUnsupported)
}

func isUnsupportedQMIForceNetworkSearchError(err error) bool {
	var qmiErr *qmi.QMIError
	if !errors.As(err, &qmiErr) || qmiErr == nil {
		return false
	}
	return qmiErr.Service == 0x03 &&
		qmiErr.MessageID == qmi.NASForceNetworkSearch &&
		(qmiErr.ErrorCode == qmi.QMIErrNotSupported ||
			qmiErr.ErrorCode == qmi.QMIErrInvalidQmiCmd ||
			qmiErr.ErrorCode == qmi.QMIErrOpDeviceUnsupported)
}

func waitQMISIMReady(ctx context.Context, deviceID string, sim qmiSIMStatusSource, opts qmiRegistrationOptions) error {
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		status, err := sim.GetSIMStatus(ctx)
		if err != nil {
			return fmt.Errorf("读取 QMI SIM 状态失败: %w", err)
		}
		if status == qmi.SIMReady {
			return nil
		}
		logger.Debug("QMI SIM 尚未 READY，等待后重试", "device", deviceID, "status", status.String(), "attempt", attempt)
		if err := sleepQMIRegistrationPoll(ctx, opts.PollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("%w: attempts=%d", errQMISIMNotReady, opts.MaxAttempts)
}

func sleepQMIRegistrationPoll(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (w *Worker) EnsureQMIRegistration(ctx context.Context, requiredForData bool) error {
	if w == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		runCtx, release, runningDone, started := w.beginQMIRegistration(ctx)
		if started {
			defer release()
			err := w.ensureQMIRegistration(runCtx, requiredForData)
			return qmiRegistrationPreferenceError(err, requiredForData)
		}

		select {
		case <-ctx.Done():
			return qmiRegistrationPreferenceError(ctx.Err(), requiredForData)
		case <-w.stop:
			return qmiRegistrationPreferenceError(context.Canceled, requiredForData)
		case <-runningDone:
			// The previous best-effort run may have completed without satisfying a
			// newly requested data connection. Re-check under a fresh ownership claim.
		}
	}
}

func (w *Worker) ensureQMIRegistration(ctx context.Context, requiredForData bool) error {
	if w == nil || w.QMICore == nil || w.Backend == nil {
		return nil
	}
	if w.Pool != nil && w.Pool.IsVoWiFiActive(w.ID) {
		logger.Debug("QMI 驻网协调跳过：VoWiFi 当前活跃", "device", w.ID)
		return nil
	}
	ctrl, ok := w.Backend.(qmiRegistrationController)
	if !ok {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, qmiRegistrationTimeout(requiredForData))
	defer cancel()

	opts := qmiRegistrationOptions{
		RegistrationOnly:   !requiredForData,
		SuppressRadioCycle: w.IsOperatorScanActive,
	}
	if w.Modem != nil && w.Modem.HasATPort() {
		opts.ATRegistration = func() (int, string, bool) {
			if !w.Modem.CanExecuteAT() {
				return 0, "", false
			}
			status, text, _, _, err := w.Modem.QueryRegistration()
			return status, text, err == nil
		}
	}
	return ensureQMIRegistration(ctx, w.ID, w.ConfigSnapshot(), w.QMICore, ctrl, opts)
}

func (w *Worker) StartQMIRegistrationReconcile(ctx context.Context, reason string) bool {
	if w == nil || w.QMICore == nil || w.Backend == nil {
		return false
	}
	return w.startQMIRegistrationReconcile(ctx, reason, func(runCtx context.Context) error {
		if err := w.ensureQMIRegistration(runCtx, false); err != nil && !errors.Is(err, errQMIRegistrationSkipped) {
			return err
		}
		return nil
	})
}

// ReconcileIdleQMIRegistration wakes a SIM that is healthy at the control
// layer but remains unregistered after a previous best-effort attempt ended.
// It deliberately excludes data-enabled, airplane and VoWiFi policies; those
// lifecycles have their own coordinators.
func (w *Worker) ReconcileIdleQMIRegistration(ctx context.Context, reason string) bool {
	if w == nil || w.QMICore == nil || w.Backend == nil {
		return false
	}
	cfg := w.ConfigSnapshot()
	if cfg.NetworkEnabled || cfg.AirplaneEnabled || cfg.VoWiFiEnabled {
		return false
	}
	if w.Pool != nil && (w.Pool.IsESIMSwitching(w.ID) || w.Pool.IsVoWiFiActive(w.ID)) {
		return false
	}
	status := w.GetCachedDeviceStatus()
	if !status.SimInserted && strings.TrimSpace(status.ICCID) == "" {
		return false
	}
	switch status.RegStatus {
	case 1, 3, 5:
		return false
	}
	return w.StartQMIRegistrationReconcile(ctx, reason)
}

func (w *Worker) startQMIRegistrationReconcile(ctx context.Context, reason string, run func(context.Context) error) bool {
	if w == nil || run == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if w.stop != nil {
		select {
		case <-w.stop:
			return false
		default:
		}
	}

	runCtx, release, _, started := w.beginQMIRegistration(ctx)
	if !started {
		logger.Debug("QMI 后台驻网协调已在运行，跳过重复触发", "device", w.ID, "reason", reason)
		return false
	}

	go func() {
		start := time.Now()
		defer release()

		logger.Debug("QMI 后台驻网协调开始", "device", w.ID, "reason", reason)
		if err := run(runCtx); err != nil {
			logger.Warn("QMI 后台驻网协调失败", "device", w.ID, "reason", reason, "elapsed_ms", time.Since(start).Milliseconds(), "err", err)
			return
		}
		logger.Debug("QMI 后台驻网协调完成", "device", w.ID, "reason", reason, "elapsed_ms", time.Since(start).Milliseconds())
	}()
	return true
}

func (w *Worker) beginQMIRegistration(parent context.Context) (context.Context, func(), <-chan struct{}, bool) {
	if w == nil {
		closed := make(chan struct{})
		close(closed)
		return context.Background(), func() {}, closed, false
	}
	if parent == nil {
		parent = context.Background()
	}

	w.qmiRegistrationMu.Lock()
	if w.qmiRegistrationInFlight {
		done := w.qmiRegistrationDone
		w.qmiRegistrationMu.Unlock()
		return nil, nil, done, false
	}

	runCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	w.qmiRegistrationInFlight = true
	w.qmiRegistrationDone = done
	w.qmiRegistrationCancel = cancel
	w.qmiRegistrationMu.Unlock()

	stopDone := make(chan struct{})
	if w.stop != nil {
		go func() {
			select {
			case <-w.stop:
				cancel()
			case <-stopDone:
			}
		}()
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			close(stopDone)
			cancel()
			w.qmiRegistrationMu.Lock()
			if w.qmiRegistrationDone == done {
				w.qmiRegistrationInFlight = false
				w.qmiRegistrationDone = nil
				w.qmiRegistrationCancel = nil
				close(done)
			}
			w.qmiRegistrationMu.Unlock()
		})
	}
	return runCtx, release, done, true
}

func (w *Worker) cancelQMIRegistration() <-chan struct{} {
	if w == nil {
		return nil
	}
	w.qmiRegistrationMu.Lock()
	cancel := w.qmiRegistrationCancel
	done := w.qmiRegistrationDone
	w.qmiRegistrationMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return done
}

func qmiRegistrationPreferenceError(err error, requiredForData bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errQMIRegistrationSkipped) {
		return nil
	}
	if requiredForData {
		return err
	}
	logger.Warn("QMI 驻网协调失败，数据网络未开启，按非致命处理", "err", err)
	return nil
}
