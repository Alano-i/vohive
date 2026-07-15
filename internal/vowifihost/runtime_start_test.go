package vowifihost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

type runtimeStartTestModem struct{}

func (runtimeStartTestModem) DeviceID() string { return "dev-1" }
func (runtimeStartTestModem) IsHealthy() bool  { return true }
func (runtimeStartTestModem) IsSimInserted() bool {
	return true
}
func (runtimeStartTestModem) QuerySIMInserted() (bool, error) { return true, nil }
func (runtimeStartTestModem) GetRegStatus() (int, string)     { return 1, "registered" }
func (runtimeStartTestModem) GetNetworkMode() string          { return "LTE" }
func (runtimeStartTestModem) ExecuteATSilent(string, time.Duration) (string, error) {
	return "", nil
}
func (runtimeStartTestModem) OpenLogicalChannel(string) (int, error) { return 0, nil }
func (runtimeStartTestModem) CloseLogicalChannel(int) error          { return nil }
func (runtimeStartTestModem) TransmitAPDU(int, string) (string, error) {
	return "", nil
}
func (runtimeStartTestModem) Stop() {}

func TestManagerStartRuntimeBuildsRequestAndClaimsInstance(t *testing.T) {
	manager := NewManager()
	deviceID := "dev-1"
	claim := manager.BeginStart(deviceID)
	if !claim.Accepted {
		t.Fatalf("BeginStart() = %+v, want accepted", claim)
	}
	wantInst := &runtimehost.Instance{}
	var captured runtimehost.StartRequest
	manager.SetRuntimeStartForTest(func(ctx context.Context, req runtimehost.StartRequest) (*runtimehost.Instance, error) {
		captured = req
		if !req.ShouldRun() {
			t.Fatal("StartRequest.ShouldRun() = false before invalidation, want true")
		}
		return wantInst, nil
	})

	result, err := manager.StartRuntime(context.Background(), RuntimeStartRequest{
		DeviceID: deviceID,
		TraceID:  "trace-1",
		Epoch:    claim.Epoch,
		Prepared: PreparedStart{
			Profile: identity.Profile{IMSI: "001010000000001"},
			Prepared: identity.PreparedSession{
				Profile: identity.Profile{IMSI: "001010000000001"},
			},
			NetworkMode: "LTE",
		},
		Modem:     runtimeStartTestModem{},
		Dataplane: runtimehost.DataplanePolicy{Mode: "userspace"},
	})
	if err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}
	if result.Instance != wantInst || result.Stale {
		t.Fatalf("StartRuntime() = %+v, want claimed instance", result)
	}
	if manager.Instance(deviceID) != wantInst {
		t.Fatal("StartRuntime() should claim instance in runtime store")
	}
	if captured.Mode != runtimehost.StartModeMain || captured.DeviceID != deviceID || captured.TraceID != "trace-1" {
		t.Fatalf("captured request identity = mode %q device %q trace %q", captured.Mode, captured.DeviceID, captured.TraceID)
	}
	if captured.SIM == nil || captured.Access == nil {
		t.Fatal("captured request should include SIM and Access adapters")
	}
	if captured.NetworkMode != "LTE" || captured.Dataplane.Mode != "userspace" {
		t.Fatalf("captured request network/dataplane = %q/%q", captured.NetworkMode, captured.Dataplane.Mode)
	}
}

func TestManagerStartRuntimeStopsStaleStartedInstance(t *testing.T) {
	manager := NewManager()
	deviceID := "dev-stale"
	claim := manager.BeginStart(deviceID)
	manager.InvalidateRuntime(deviceID, "test")
	manager.SetRuntimeStartForTest(func(ctx context.Context, req runtimehost.StartRequest) (*runtimehost.Instance, error) {
		if req.ShouldRun() {
			t.Fatal("StartRequest.ShouldRun() = true after invalidation, want false")
		}
		return &runtimehost.Instance{}, nil
	})

	result, err := manager.StartRuntime(context.Background(), RuntimeStartRequest{
		DeviceID: deviceID,
		TraceID:  "trace-stale",
		Epoch:    claim.Epoch,
		Prepared: PreparedStart{
			Profile:     identity.Profile{IMSI: "001010000000001"},
			Prepared:    identity.PreparedSession{Profile: identity.Profile{IMSI: "001010000000001"}},
			NetworkMode: "LTE",
		},
		Modem: runtimeStartTestModem{},
	})
	if err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}
	if !result.Stale {
		t.Fatalf("StartRuntime() stale = false, want true")
	}
	if manager.Active(deviceID) {
		t.Fatal("stale started instance should not become active")
	}
}

func TestManagerStartRuntimeRecordsFailureState(t *testing.T) {
	manager := NewManager()
	deviceID := "dev-failed"
	claim := manager.BeginStart(deviceID)
	if !claim.Accepted {
		t.Fatalf("BeginStart() = %+v, want accepted", claim)
	}
	manager.SetRuntimeStartForTest(func(ctx context.Context, req runtimehost.StartRequest) (*runtimehost.Instance, error) {
		return nil, errors.New("epdg tunnel establishment timed out after 45s")
	})

	_, err := manager.StartRuntime(context.Background(), RuntimeStartRequest{
		DeviceID: deviceID,
		TraceID:  "trace-failed",
		Epoch:    claim.Epoch,
		Prepared: PreparedStart{
			Profile:  identity.Profile{IMSI: "001010000000001"},
			Prepared: identity.PreparedSession{Profile: identity.Profile{IMSI: "001010000000001"}},
			StartupState: runtimehost.State{
				DeviceID:    deviceID,
				SIMReady:    true,
				AccessReady: true,
			},
		},
		Modem: runtimeStartTestModem{},
	})
	if err == nil {
		t.Fatal("StartRuntime() error = nil, want failure")
	}
	if manager.RuntimeStore().Starting(deviceID) {
		t.Fatal("runtime starting flag should be cleared after failure")
	}
	state, ok := manager.State(deviceID)
	if !ok {
		t.Fatal("expected failed runtime state to remain visible")
	}
	if state.Phase != runtimehost.PhaseFailed {
		t.Fatalf("Phase = %q, want %q", state.Phase, runtimehost.PhaseFailed)
	}
	if state.LastError != "epdg tunnel establishment timed out after 45s" {
		t.Fatalf("LastError = %q", state.LastError)
	}
}
