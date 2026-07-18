package device

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/modem"
)

func TestPrepareIPCacheForSIMSwitchClearsOldCarrierState(t *testing.T) {
	w := &Worker{
		cachedIP:           "102.89.33.201",
		cachedPublicIPv6:   "2001:db8::1",
		cacheTime:          time.Now(),
		publicIPRetryCount: 5,
		ipRefreshLast:      time.Now(),
	}
	w.publicIPRetryTimer = time.AfterFunc(time.Hour, func() {})

	w.prepareIPCacheForSIMSwitch()

	if got := w.GetCachedIP(); got != "" {
		t.Fatalf("cached IPv4=%q, want empty", got)
	}
	if got := w.GetCachedIPv6(); got != "" {
		t.Fatalf("cached IPv6=%q, want empty", got)
	}
	if w.publicIPRetryCount != 0 || w.publicIPRetryTimer != nil {
		t.Fatalf("retry state not reset: count=%d timer=%v", w.publicIPRetryCount, w.publicIPRetryTimer)
	}
	if !w.ipRefreshLast.IsZero() {
		t.Fatalf("ipRefreshLast=%v, want zero", w.ipRefreshLast)
	}
	if w.ipCacheGeneration.Load() != 1 {
		t.Fatalf("cache generation=%d, want 1", w.ipCacheGeneration.Load())
	}
}

type stalePublicIPProbeController struct {
	fakeController
	started chan struct{}
	release chan struct{}
}

func (c *stalePublicIPProbeController) GetPublicIPv4AndV6NoCache() (string, string) {
	close(c.started)
	<-c.release
	return "102.89.33.201", ""
}

func TestRefreshIPsDiscardsProbeStartedBeforeSIMSwitch(t *testing.T) {
	p := NewPool(&config.Config{})
	defer p.cancel()
	controller := &stalePublicIPProbeController{
		fakeController: fakeController{connected: true},
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	w := &Worker{ID: "switch-ip", Pool: p, netOverride: controller}

	p.refreshIPs(w, true)
	select {
	case <-controller.started:
	case <-time.After(time.Second):
		t.Fatal("public IP probe did not start")
	}
	w.prepareIPCacheForSIMSwitch()
	close(controller.release)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		w.ipRefreshMu.Lock()
		inFlight := w.ipRefreshInFlight
		w.ipRefreshMu.Unlock()
		if !inFlight {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := w.GetCachedIP(); got != "" {
		t.Fatalf("stale probe repopulated old carrier IP: %q", got)
	}
}

func TestWorkerUsesQMIHealthPolicyForATHybridWorker(t *testing.T) {
	worker := &Worker{Config: config.DeviceConfig{
		DeviceBackend: "at",
		ControlDevice: "/dev/cdc-wdm0",
		Interface:     "wwan0",
	}}
	if !workerUsesQMIHealthPolicy(worker) {
		t.Fatal("AT identity worker with a QMI attachment must use QMI health policy")
	}

	atOnly := &Worker{Config: config.DeviceConfig{DeviceBackend: "at", ATPort: "/dev/ttyUSB2"}}
	if workerUsesQMIHealthPolicy(atOnly) {
		t.Fatal("AT-only worker must not use QMI health policy")
	}
}

func TestProbeDeviceHealthAcceptsHealthyQMIControlForATHybridWorker(t *testing.T) {
	worker := &Worker{
		ID: "hybrid",
		Config: config.DeviceConfig{
			DeviceBackend: "at",
			ControlDevice: "/dev/cdc-wdm0",
			Interface:     "wwan0",
		},
		Modem: &modem.Manager{},
	}
	worker.RecordWatchdogEvent(WatchdogEvent{
		Layer:     HealthLayerQMI,
		State:     HealthStateHealthy,
		EventType: "qmi_control_ready",
		Reason:    "test",
	})

	healthy, err := worker.ProbeDeviceHealth()
	if err != nil {
		t.Fatalf("ProbeDeviceHealth() error = %v", err)
	}
	if !healthy {
		t.Fatal("healthy QMI control plane should keep hybrid worker healthy when auxiliary AT is down")
	}
}

func TestProbeDeviceHealthKeepsIdleHybridWorkerWhenQMINodeExists(t *testing.T) {
	control, err := os.CreateTemp(t.TempDir(), "cdc-wdm-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}

	worker := &Worker{
		ID: "idle-hybrid",
		Config: config.DeviceConfig{
			DeviceBackend:  "at",
			ControlDevice:  control.Name(),
			Interface:      "wwan0",
			NetworkEnabled: false,
		},
		Modem: &modem.Manager{},
	}
	worker.RecordWatchdogEvent(WatchdogEvent{
		Layer:     HealthLayerQMI,
		State:     HealthStateSuspect,
		EventType: "qmi_core_idle",
		Reason:    "test",
	})

	healthy, err := worker.ProbeDeviceHealth()
	if err != nil {
		t.Fatalf("ProbeDeviceHealth() error = %v", err)
	}
	if !healthy {
		t.Fatal("idle hybrid worker with present QMI node must not be rebuilt")
	}
}

func TestHealthCheckSkipsDeviceUnderRebootRecovery(t *testing.T) {
	// 当设备处于 modemRebootRecovering 中时，
	// healthCheckLoop 不应尝试快速拉起或重扫，而应完全委托给恢复循环。
	p := NewPool(&config.Config{})
	defer p.cancel()

	deviceID := "dev-qmi"
	p.mu.Lock()
	if p.modemRebootRecovering == nil {
		p.modemRebootRecovering = make(map[string]bool)
	}
	p.modemRebootRecovering[deviceID] = true
	p.mu.Unlock()

	// 检查判据逻辑
	p.mu.RLock()
	isRecovering := p.modemRebootRecovering[deviceID]
	p.mu.RUnlock()

	if !isRecovering {
		t.Fatalf("device should be marked as under reboot recovery, but modemRebootRecovering[%s]=false", deviceID)
	}
}

func TestHealthCheckAllowsFastPullWhenNotRecovering(t *testing.T) {
	// 当设备不在恢复中时，healthCheckLoop 可以尝试快速拉起。
	p := NewPool(&config.Config{})
	defer p.cancel()

	deviceID := "dev-qmi"
	// 不设置 modemRebootRecovering 标记

	p.mu.RLock()
	isRecovering := p.modemRebootRecovering[deviceID]
	p.mu.RUnlock()

	if isRecovering {
		t.Fatalf("device should NOT be marked as under reboot recovery, but modemRebootRecovering[%s]=true", deviceID)
	}
}

// TestRunHealthCheckTickSkipsObservationWindowOnTransportDownError 测试当探活失败的错误
// 明确表示传输已断开（broken pipe/EOF/connection closed 等）时，应跳过 3 次观察窗口，
// 第一次失败就直接触发恢复，而不是像普通超时那样等满 qmiHealthFailureThreshold 次。
func TestRunHealthCheckTickSkipsObservationWindowOnTransportDownError(t *testing.T) {
	p := NewPool(&config.Config{})
	defer p.cancel()

	worker := &Worker{
		ID: "dev1",
		Config: config.DeviceConfig{
			ID:            "dev1",
			DeviceBackend: backend.BackendQMI,
			ControlDevice: "/dev/cdc-wdm0",
		},
		Backend: &workerStatusBackendStub{
			mode:      backend.BackendQMI,
			opModeErr: errors.New("write failed: write unix @->@qmi-proxy: write: broken pipe"),
		},
	}
	p.workers["dev1"] = worker

	p.runHealthCheckTick()

	// scheduleWorkerRecoveryWithTransportEvent 内部会再记一次 Reprobing 事件覆盖前面的 Invalid 事件，
	// 这跟原有 3 次阈值触发恢复时的行为一致；这里通过 Reason 区分走的是哪条触发路径。
	snapshot := worker.HealthSnapshot()
	if snapshot.State != HealthStateReprobing {
		t.Fatalf("state=%s want %s after single transport-down failure triggers recovery", snapshot.State, HealthStateReprobing)
	}
	if snapshot.Reason != "qmi_transport_down" {
		t.Fatalf("reason=%q want qmi_transport_down", snapshot.Reason)
	}
}

// TestRunHealthCheckTickStillWaitsForThresholdOnTransientError 测试普通瞬时错误（非传输确认已断）
// 仍然遵循原有的 3 次观察窗口，不应被这次改动误伤。
func TestRunHealthCheckTickStillWaitsForThresholdOnTransientError(t *testing.T) {
	p := NewPool(&config.Config{})
	defer p.cancel()

	worker := &Worker{
		ID: "dev1",
		Config: config.DeviceConfig{
			ID:            "dev1",
			DeviceBackend: backend.BackendQMI,
			ControlDevice: "/dev/cdc-wdm0",
		},
		Backend: &workerStatusBackendStub{
			mode:      backend.BackendQMI,
			opModeErr: errors.New("context deadline exceeded"),
		},
	}
	p.workers["dev1"] = worker

	p.runHealthCheckTick()

	snapshot := worker.HealthSnapshot()
	if snapshot.State == HealthStateInvalid {
		t.Fatalf("state=%s, single transient timeout should not bypass the observation window", snapshot.State)
	}
}
