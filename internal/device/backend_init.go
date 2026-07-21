package device

import (
	"fmt"
	"strings"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/modem"
)

func newWorkerBackendStrict(deviceID, backendMode, controlDevice string, m *modem.Manager, source backend.QMISource, mbimSource backend.MBIMSource) (backend.DeviceBackend, error) {
	be, err := backend.NewBackend(backendMode, controlDevice, m, source, mbimSource)
	if err != nil {
		prefix := ""
		if id := strings.TrimSpace(deviceID); id != "" {
			prefix = fmt.Sprintf("[%s] ", id)
		}
		return nil, fmt.Errorf("%s初始化 %s 后端失败: %w", prefix, backendMode, err)
	}
	return be, nil
}

func backendUsesATRuntime(mode string) bool {
	return backend.NormalizeBackendMode(mode) == backend.BackendAT
}

// workerNeedsATRuntime reflects the DJI runtime model: QMI owns data and radio
// control, while the auxiliary AT port remains open for eSIM/APDU and diagnostics.
func workerNeedsATRuntime(cfg config.DeviceConfig) bool {
	if backendUsesATRuntime(resolvedBackendMode(cfg)) {
		return true
	}
	if strings.TrimSpace(cfg.ATPort) == "" && strings.TrimSpace(cfg.ManagePort) == "" {
		return false
	}
	return deriveESIMTransport(cfg) == config.ESIMTransportAT
}
