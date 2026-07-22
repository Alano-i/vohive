package device

import (
	"testing"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vowifi-go/runtimehost"
)

func TestVoWiFiTeardownPathsRestoreSMSState(t *testing.T) {
	paths := []struct {
		name     string
		teardown func(p *Pool, deviceID string) bool
	}{
		{
			name: "reconnect",
			teardown: func(p *Pool, deviceID string) bool {
				return p.teardownVoWiFiForReconnect(deviceID)
			},
		},
		{
			name: "switch",
			teardown: func(p *Pool, deviceID string) bool {
				return p.voWiFiHost().TeardownForSwitch(p.ctx, deviceID)
			},
		},
	}

	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPool(&config.Config{})

			deviceID := "wwan0"
			worker := &Worker{ID: deviceID}
			worker.setRestoreNetworkAfterVoWiFi(true)
			p.workers[deviceID] = worker
			p.voWiFiRuntimeStore().SetInstance(deviceID, &runtimehost.Instance{})

			if ok := tc.teardown(p, deviceID); !ok {
				t.Fatal("expected teardown to report existing app")
			}
			if p.workers[deviceID].shouldRestoreNetworkAfterVoWiFi() {
				t.Fatal("expected restoreNetworkAfterVoWiFi to be cleared")
			}
		})
	}
}

func TestDisableVoWiFiRestoresSMSStateWithoutApp(t *testing.T) {
	p := NewPool(&config.Config{})

	deviceID := "wwan0"
	worker := &Worker{ID: deviceID, smsMode: smsModeVoWiFi}
	worker.setRestoreNetworkAfterVoWiFi(true)
	p.workers[deviceID] = worker

	if err := p.DisableVoWiFi(deviceID); err != nil {
		t.Fatalf("DisableVoWiFi() error = %v", err)
	}

	worker = p.workers[deviceID]
	if worker.shouldRestoreNetworkAfterVoWiFi() {
		t.Fatal("expected restoreNetworkAfterVoWiFi to be cleared")
	}
	if worker.smsMode != smsModeAT {
		t.Fatalf("expected smsMode to restore to %v, got %v", smsModeAT, worker.smsMode)
	}
}
