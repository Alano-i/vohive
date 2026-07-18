package qmicore

import (
	"testing"

	qmimanager "github.com/iniwex5/quectel-qmi-go/pkg/manager"
	"github.com/iniwex5/vohive/internal/config"
)

func TestBuildQMIManagerConfigSkipsUIMForHybridDJIModem(t *testing.T) {
	cfg := config.DeviceConfig{
		DeviceBackend: "qmi",
		ATPort:        "/dev/ttyUSB10",
	}
	got := buildQMIManagerConfig(cfg, qmimanager.ModemDevice{})
	if !got.DisableUIMAtStart {
		t.Fatal("hybrid DJI modem should use its auxiliary AT port for UICC operations")
	}
	if !got.SerializeServiceAllocation {
		t.Fatal("hybrid DJI modem should allocate QMI services sequentially")
	}
}

func TestBuildQMIManagerConfigKeepsUIMForQMIOnlyModem(t *testing.T) {
	cfg := config.DeviceConfig{
		DeviceBackend: "qmi",
		ControlDevice: "/dev/cdc-wdm0",
		ESIMTransport: config.ESIMTransportQMI,
	}
	got := buildQMIManagerConfig(cfg, qmimanager.ModemDevice{})
	if got.DisableUIMAtStart {
		t.Fatal("QMI-only modem still requires QMI UIM during startup")
	}
}
