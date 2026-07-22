package device

import (
	"fmt"
	"strings"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/pkg/logger"
)

func replaceBackendDuringATRuntimeRecovery(cfg config.DeviceConfig) bool {
	return resolvedBackendMode(cfg) == backend.BackendAT
}

// EnsureESIMRuntime makes sure the APDU runtime used by eSIM operations is
// actually usable. DJI/Baiwang devices keep the worker alive when the auxiliary
// AT port drops because QMI remains the authoritative lifecycle channel. That is
// correct for overview/data state, but any eSIM operation over AT still needs a
// fresh modem.Manager.
func (p *Pool) EnsureESIMRuntime(deviceID, reason string) error {
	if p == nil {
		return fmt.Errorf("设备池未初始化")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "esim_runtime_preflight"
	}
	worker := p.GetWorker(deviceID)
	if worker == nil {
		return fmt.Errorf("设备未找到或未运行")
	}
	cfg := worker.ConfigSnapshot()
	if config.NormalizeESIMTransport(cfg.ESIMTransport) != config.ESIMTransportAT &&
		deriveESIMTransport(cfg) != config.ESIMTransportAT {
		return nil
	}
	if worker.Modem != nil && worker.Modem.CanExecuteAT() && worker.EsimMgr != nil {
		return nil
	}
	if worker.EsimMgr != nil && worker.Modem == nil && strings.TrimSpace(worker.ResolvedATPort()) == "" {
		return nil
	}
	return p.rebuildATRuntimeForWorker(worker, reason)
}

// EnsureESIMSwitchRuntime keeps the original switch-specific API while routing
// through the generic eSIM runtime preflight used by read and write endpoints.
func (p *Pool) EnsureESIMSwitchRuntime(deviceID string) error {
	return p.EnsureESIMRuntime(deviceID, "esim_switch_preflight")
}

func (p *Pool) rebuildATRuntimeForWorker(worker *Worker, reason string) error {
	if worker == nil {
		return fmt.Errorf("设备未找到或未运行")
	}
	worker.atRuntimeMu.Lock()
	defer worker.atRuntimeMu.Unlock()

	if worker.Modem != nil && worker.Modem.CanExecuteAT() && worker.EsimMgr != nil {
		return nil
	}

	port := strings.TrimSpace(worker.ResolvedATPort())
	if port == "" {
		return fmt.Errorf("AT 控制口不可用，无法恢复 eSIM APDU 通道")
	}

	cfg := worker.ConfigSnapshot()
	cfg.ATPort = port
	cfg.ManagePort = port
	replaceBackend := replaceBackendDuringATRuntimeRecovery(cfg)
	// This manager is an auxiliary AT runtime. The worker's persisted data
	// backend remains QMI/MBIM and is restored below after eSIM initialization.
	cfg.DeviceBackend = backend.BackendAT
	cfg.ESIMTransport = config.ESIMTransportAT

	nextModem, err := modem.New(cfg)
	if err != nil {
		return fmt.Errorf("重建 AT 管理器失败: %w", err)
	}
	if worker.APDUArbiter != nil {
		nextModem.SetAPDUArbiter(worker.APDUArbiter)
	}
	nextModem.SetOnDisconnectWithReason(func(disconnectReason string) {
		devID := worker.ID
		if current := p.GetWorker(devID); current != worker {
			logger.Debug("忽略已移除 Worker 的 AT 掉线回调", "device", devID, "reason", disconnectReason)
			return
		}
		if workerUsesQMIHealthPolicy(worker) {
			logger.Warn("AT 辅助控制口掉线，保留 QMI Worker 与 SIM 身份缓存",
				"device", devID,
				"reason", disconnectReason,
				"network_connected", worker.NetworkConnected())
			return
		}
		if strings.TrimSpace(disconnectReason) == "" {
			disconnectReason = "modem_disconnect"
		}
		logger.Warn(fmt.Sprintf("[%s] 检测到模块掉线，将进入重启恢复扫描", devID), "reason", disconnectReason)
		p.scheduleATDisconnectRecovery(devID, disconnectReason)
	})
	if err := nextModem.Start(); err != nil {
		nextModem.Stop()
		return fmt.Errorf("启动 AT 管理器失败: %w", err)
	}
	if !p.isCurrentWorker(worker) {
		nextModem.Stop()
		return fmt.Errorf("设备运行时已变化，请重试")
	}

	oldBackend := worker.Backend
	nextBackend := oldBackend
	if replaceBackend {
		nextBackend, err = backend.NewBackend(backend.BackendAT, cfg.ControlDevice, nextModem, nil, nil)
		if err != nil {
			nextModem.Stop()
			return fmt.Errorf("重建 AT 后端失败: %w", err)
		}
	}

	onBeforeSwitch, onAfterSwitch, onSwitchFailed, onSwitchDegraded, onSwitchPhase := p.newESIMSwitchCallbacks(worker)
	runtimeWorker := &Worker{
		ID:               worker.ID,
		Config:           cfg,
		Modem:            nextModem,
		Backend:          nextBackend,
		ESIMQMITransport: worker.ESIMQMITransport,
		APDUArbiter:      worker.APDUArbiter,
	}
	nextESIMMgr, err := newESIMManagerForWorker(runtimeWorker, worker.ESIMQMITransport, onBeforeSwitch, onAfterSwitch, onSwitchFailed, onSwitchDegraded, onSwitchPhase)
	if err != nil {
		if replaceBackend && nextBackend != nil {
			_ = nextBackend.Close()
		}
		nextModem.Stop()
		return fmt.Errorf("重建 eSIM 管理器失败: %w", err)
	}

	p.mu.Lock()
	if current := p.workers[worker.ID]; current != worker {
		p.mu.Unlock()
		if replaceBackend && nextBackend != nil {
			_ = nextBackend.Close()
		}
		nextModem.Stop()
		return fmt.Errorf("设备运行时已变化，请重试")
	}
	oldModem := worker.Modem
	oldESIMMgr := worker.EsimMgr
	worker.Modem = nextModem
	worker.Backend = nextBackend
	worker.updateConfig(func(cfg *config.DeviceConfig) {
		cfg.ATPort = port
		cfg.ManagePort = port
	})
	worker.EsimMgr = nextESIMMgr
	p.mu.Unlock()
	p.bindModemReadyIndications(worker)

	if replaceBackend && oldBackend != nil {
		_ = oldBackend.Close()
	}
	if oldModem != nil && oldModem != nextModem {
		oldModem.Stop()
	}
	if oldESIMMgr != nil && oldESIMMgr != nextESIMMgr {
		oldESIMMgr.Close()
	}

	logger.Info("已重建 AT/eSIM 运行时",
		"device", worker.ID,
		"reason", reason,
		"at_port", port,
		"data_backend", resolvedBackendMode(worker.ConfigSnapshot()),
		"esim_transport", config.ESIMTransportAT)
	return nil
}
