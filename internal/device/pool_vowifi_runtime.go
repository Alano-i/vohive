package device

import (
	"context"
	"fmt"
	"strings"
	"time"

	qmimanager "github.com/iniwex5/quectel-qmi-go/pkg/manager"

	"github.com/iniwex5/vohive/pkg/logger"
)

func waitForCondition(ctx context.Context, interval time.Duration, check func() bool) error {
	if check() {
		return nil
	}
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if check() {
				return nil
			}
		}
	}
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *Pool) waitWorkerReady(deviceID string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()
	return waitForCondition(waitCtx, 200*time.Millisecond, func() bool {
		w := p.GetWorker(deviceID)
		if w == nil {
			return false
		}
		return w.IsDeviceHealthy()
	})
}

func (p *Pool) waitRadioRecoveryReady(deviceID string, timeout time.Duration) error {
	w := p.GetWorker(deviceID)
	if w == nil {
		return fmt.Errorf("设备 %s 不存在", deviceID)
	}
	waitCtx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	// Hybrid DJI/Baiwang workers keep QMI as the authoritative control plane
	// while AT is only an auxiliary status channel. If that AT port drops, do
	// not let its stale readiness implementation hide a healthy QMI core.
	if w.QMICore != nil {
		return w.QMICore.WaitIdentityReady(waitCtx)
	}
	if b, ok := w.Backend.(interface {
		GetUIMReadiness(context.Context) (qmimanager.UIMReadiness, error)
	}); ok {
		return waitForCondition(waitCtx, 500*time.Millisecond, func() bool {
			rdy, err := b.GetUIMReadiness(waitCtx)
			if err != nil {
				return false
			}
			return rdy.Reason == qmimanager.UIMReadinessReady
		})
	}
	if w.Modem != nil {
		if !w.Modem.WaitReady(timeout) {
			return context.DeadlineExceeded
		}
		return nil
	}
	return nil
}

func (p *Pool) waitQMICoreReady(deviceID string, timeout time.Duration) error {
	w := p.GetWorker(deviceID)
	if w == nil {
		return fmt.Errorf("设备 %s 不存在", deviceID)
	}
	waitCtx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	if w.QMICore != nil {
		return w.QMICore.WaitIdentityReady(waitCtx)
	}
	if b, ok := w.Backend.(interface {
		GetUIMReadiness(context.Context) (qmimanager.UIMReadiness, error)
	}); ok {
		return waitForCondition(waitCtx, 500*time.Millisecond, func() bool {
			rdy, err := b.GetUIMReadiness(waitCtx)
			if err != nil {
				return false
			}
			return rdy.Reason == qmimanager.UIMReadinessReady
		})
	}
	return nil
}

func (p *Pool) WaitQMICoreReady(deviceID string, timeout time.Duration) error {
	return p.waitQMICoreReady(deviceID, timeout)
}

func (p *Pool) waitQMIControlReady(deviceID string, timeout time.Duration) error {
	w := p.GetWorker(deviceID)
	if w == nil {
		return fmt.Errorf("设备 %s 不存在", deviceID)
	}
	waitCtx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	if w.QMICore != nil {
		return w.QMICore.WaitControlReady(waitCtx)
	}
	if b, ok := w.Backend.(interface {
		GetUIMReadiness(context.Context) (qmimanager.UIMReadiness, error)
	}); ok {
		return waitForCondition(waitCtx, 500*time.Millisecond, func() bool {
			rdy, err := b.GetUIMReadiness(waitCtx)
			if err != nil {
				return false
			}
			return rdy.ControlReady
		})
	}
	return nil
}

func (p *Pool) WaitQMIControlReady(deviceID string, timeout time.Duration) error {
	return p.waitQMIControlReady(deviceID, timeout)
}

func (p *Pool) WaitWorkerReady(deviceID string, timeout time.Duration) error {
	return p.waitWorkerReady(deviceID, timeout)
}

func (p *Pool) WorkerExists(deviceID string) bool {
	return p.GetWorker(deviceID) != nil
}

func (p *Pool) IsSwitching(deviceID string) bool {
	return p.IsESIMSwitching(deviceID)
}

// enableVoWiFiWhenReady waits for readiness, then submits enable through the lifecycle controller.
// Do not call this from controller run paths that already hold the per-device lifecycle mutex.
func (p *Pool) enableVoWiFiWhenReady(deviceID string, timeout time.Duration, reason string) error {
	if err := p.waitQMICoreReady(deviceID, timeout); err != nil {
		return fmt.Errorf("等待设备 %s 身份就绪失败(%s): %w", deviceID, reason, err)
	}
	if err := p.waitWorkerReady(deviceID, timeout); err != nil {
		return fmt.Errorf("等待设备 %s 就绪失败(%s): %w", deviceID, reason, err)
	}
	return p.EnableVoWiFi(deviceID)
}

func (p *Pool) EnableVoWiFi(deviceID string) error {
	if p.IsESIMSwitching(deviceID) {
		return fmt.Errorf("设备 %s 正在切卡，暂不允许启动 VoWiFi", deviceID)
	}
	return p.voWiFiHost().Enable(p.ctx, deviceID)
}

// RequestEnableVoWiFi accepts the persisted VoWiFi desired state and starts the
// existing desired-state recovery path in the background. Establishing an ePDG
// tunnel can take tens of seconds and may legitimately enter the retry loop, so
// an HTTP toggle must not stay blocked until the first runtime attempt finishes.
func (p *Pool) RequestEnableVoWiFi(deviceID string) error {
	if p == nil {
		return fmt.Errorf("设备池未就绪")
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return fmt.Errorf("设备 ID 不能为空")
	}
	if p.GetWorker(deviceID) == nil {
		return fmt.Errorf("设备 %s 不存在", deviceID)
	}
	if p.IsVoWiFiActive(deviceID) || p.voWiFiHost().Starting(deviceID) {
		return nil
	}

	// A deliberate click is an immediate retry request, so discard any backoff
	// left by an earlier failed automatic attempt before scheduling this one.
	p.clearDesiredVoWiFiRecoverState(deviceID)
	if !p.scheduleDesiredVoWiFiRecover(deviceID, "user_enable", time.Now()) {
		logger.Debug("VoWiFi 用户开启请求已保存，后台启动由后续目标态协调接管", "device", deviceID)
	}
	return nil
}
