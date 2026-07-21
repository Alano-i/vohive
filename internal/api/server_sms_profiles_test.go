package api

import (
	"testing"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/device"
)

func TestSMSWorkerESIMEnabledUsesManagedCapability(t *testing.T) {
	worker := &device.Worker{Config: config.DeviceConfig{ESIMEnabled: true}}

	if smsWorkerESIMEnabled(worker, config.DeviceConfig{ESIMEnabled: false}, true) {
		t.Fatal("managed physical-SIM device must not be scanned as eSIM")
	}
	if !smsWorkerESIMEnabled(worker, config.DeviceConfig{ESIMEnabled: true}, true) {
		t.Fatal("managed eSIM device was not recognized")
	}
	if !smsWorkerESIMEnabled(worker, config.DeviceConfig{}, false) {
		t.Fatal("runtime eSIM capability should be used without managed config")
	}
}
