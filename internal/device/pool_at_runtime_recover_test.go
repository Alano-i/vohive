package device

import (
	"testing"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
)

func TestATRuntimeRecoveryPreservesQMIDataBackend(t *testing.T) {
	cfg := config.DeviceConfig{
		DeviceBackend: backend.BackendQMI,
		ControlDevice: "/dev/cdc-wdm2",
		ATPort:        "/dev/ttyUSB10",
	}
	if replaceBackendDuringATRuntimeRecovery(cfg) {
		t.Fatal("QMI worker recovery must preserve the QMI data backend")
	}
}

func TestATRuntimeRecoveryReplacesATBackend(t *testing.T) {
	cfg := config.DeviceConfig{DeviceBackend: backend.BackendAT, ATPort: "/dev/ttyUSB2"}
	if !replaceBackendDuringATRuntimeRecovery(cfg) {
		t.Fatal("AT worker recovery must rebuild its AT backend")
	}
}
