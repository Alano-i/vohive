package device

import (
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/pkg/logger"
)

// SwitchWorkerBackend switches between AT and QMI without tearing down the
// shared QMI core. DJI modules expose both transports at the same time, and a
// full teardown can leave their firmware unable to allocate another QMI client.
func (p *Pool) SwitchWorkerBackend(deviceID string, cfg config.DeviceConfig) error {
	if p == nil {
		return fmt.Errorf("设备池未初始化")
	}

	targetMode := resolvedBackendMode(cfg)
	if targetMode != backend.BackendAT && targetMode != backend.BackendQMI {
		return fmt.Errorf("不支持原地切换到 %s 后端", targetMode)
	}

	worker := p.GetWorker(deviceID)
	if worker == nil {
		return fmt.Errorf("设备未找到或未运行")
	}

	// A QMI worker may intentionally start without opening its auxiliary AT
	// port. Build that runtime before selecting AT as the primary backend.
	if targetMode == backend.BackendAT &&
		(worker.Modem == nil || !worker.Modem.CanExecuteAT()) {
		if err := p.rebuildATRuntimeForWorker(worker, "backend_switch_to_at"); err != nil {
			return err
		}
		worker = p.GetWorker(deviceID)
		if worker == nil {
			return fmt.Errorf("设备在切换 AT 运行时期间已离线")
		}
	}

	worker.atRuntimeMu.Lock()
	defer worker.atRuntimeMu.Unlock()

	if targetMode == backend.BackendQMI && worker.QMICore == nil {
		return fmt.Errorf("QMI Core 不可用，无法原地切换到 QMI")
	}
	if targetMode == backend.BackendAT &&
		(worker.Modem == nil || !worker.Modem.CanExecuteAT()) {
		return fmt.Errorf("AT 控制口不可用，无法原地切换到 AT")
	}

	nextBackend, err := newWorkerBackendStrict(
		deviceID,
		targetMode,
		cfg.ControlDevice,
		worker.Modem,
		worker.QMICore,
		worker.MBIMCore,
	)
	if err != nil {
		return err
	}

	previousBackend := worker.Backend
	p.mu.Lock()
	if current := p.workers[deviceID]; current != worker {
		p.mu.Unlock()
		_ = nextBackend.Close()
		return fmt.Errorf("设备运行时已变化，请重试")
	}
	worker.Backend = nextBackend
	worker.Config = cfg
	p.mu.Unlock()

	p.configureWorkerSMSRuntime(worker, targetMode)
	if previousBackend != nil && previousBackend != nextBackend {
		_ = previousBackend.Close()
	}
	p.resolveAndApplyPolicy(worker, "backend_switch")

	logger.Info("设备后端已原地切换",
		"device", deviceID,
		"backend", targetMode,
		"qmi_core_preserved", worker.QMICore != nil,
		"esim_transport", deriveESIMTransport(cfg))
	return nil
}

func (p *Pool) configureWorkerSMSRuntime(worker *Worker, backendMode string) {
	if worker == nil {
		return
	}

	switch strings.ToLower(strings.TrimSpace(backendMode)) {
	case backend.BackendQMI:
		worker.smsMode = smsModeQMI
		if worker.Modem != nil {
			worker.Modem.SetNewSMSHandler(nil)
			worker.Modem.SetSMSCallback(nil)
			worker.Modem.SetDisableURCRead(true)
		}
	case backend.BackendAT:
		worker.smsMode = smsModeAT
		if worker.Modem != nil {
			worker.Modem.SetNewSMSHandler(nil)
			worker.Modem.SetDisableURCRead(false)
			worker.Modem.SetSMSCallback(func(sender, content string, timestamp time.Time) {
				worker.processSMS(sender, content, timestamp)
			})
		}
	}
}
