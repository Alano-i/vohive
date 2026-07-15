package manager

import "testing"

func TestDisconnectWhileCoreIsStartingIsIdempotent(t *testing.T) {
	m := &Manager{
		state:             StateConnecting,
		desiredConnection: true,
	}

	if err := m.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v, want nil before data call exists", err)
	}
	if m.desiredConnection {
		t.Fatal("Disconnect() left desiredConnection enabled")
	}
}

func TestDisconnectWithoutCoreRejectsOwnedDataCall(t *testing.T) {
	m := &Manager{
		state:    StateConnected,
		handleV4: 1,
	}

	if err := m.Disconnect(); err == nil {
		t.Fatal("Disconnect() error = nil, want error for inconsistent owned data call")
	}
}
