package device

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	qmimanager "github.com/iniwex5/quectel-qmi-go/pkg/manager"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	qmicore "github.com/iniwex5/vohive/internal/qmi"
)

type mockReadinessBackend struct {
	backend.DeviceBackend
	r qmimanager.UIMReadiness
	e error
}

func (m *mockReadinessBackend) GetUIMReadiness(ctx context.Context) (qmimanager.UIMReadiness, error) {
	return m.r, m.e
}

func TestWaitUIMIdentityReady_ReadyWithIdentity(t *testing.T) {
	p := NewPool(&config.Config{})
	w := &Worker{ID: "test-mbim", Backend: &mockReadinessBackend{
		r: qmimanager.UIMReadiness{
			Reason: qmimanager.UIMReadinessReady,
			ICCID:  "123",
			IMSI:   "456",
		},
	}}
	p.mu.Lock()
	p.workers["test-mbim"] = w
	p.mu.Unlock()

	err := p.WaitQMICoreReady("test-mbim", 1*time.Second)
	if err != nil {
		t.Fatalf("expected nil error when identity is ready, got %v", err)
	}
}

func TestWaitQMICoreReady_IdentityEmpty(t *testing.T) {
	p := NewPool(&config.Config{})
	w := &Worker{ID: "test-mbim", Backend: &mockReadinessBackend{
		r: qmimanager.UIMReadiness{
			Reason: qmimanager.UIMReadinessIdentityEmpty,
		},
	}}
	p.mu.Lock()
	p.workers["test-mbim"] = w
	p.mu.Unlock()

	err := p.WaitQMICoreReady("test-mbim", 100*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout (DeadlineExceeded) when identity is empty, got %v", err)
	}
}

func TestWaitQMICoreReady_CardAbsent(t *testing.T) {
	p := NewPool(&config.Config{})
	w := &Worker{ID: "test-mbim", Backend: &mockReadinessBackend{
		r: qmimanager.UIMReadiness{
			Reason:      qmimanager.UIMReadinessCardAbsent,
			CardPresent: false,
		},
	}}
	p.mu.Lock()
	p.workers["test-mbim"] = w
	p.mu.Unlock()

	err := p.WaitQMICoreReady("test-mbim", 100*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout (DeadlineExceeded) when card is absent, got %v", err)
	}
}

func TestWaitQMICoreReady_TransportFatal(t *testing.T) {
	p := NewPool(&config.Config{})
	w := &Worker{ID: "test-mbim", Backend: &mockReadinessBackend{
		r: qmimanager.UIMReadiness{
			Reason:         qmimanager.UIMReadinessTransportFatal,
			TransportReady: false,
		},
	}}
	p.mu.Lock()
	p.workers["test-mbim"] = w
	p.mu.Unlock()

	err := p.WaitQMICoreReady("test-mbim", 100*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout (DeadlineExceeded) when transport is fatal, got %v", err)
	}
}

func TestWaitQMICoreReadyHybridWorkerPrefersQMICoreOverAuxiliaryAT(t *testing.T) {
	p := NewPool(&config.Config{})
	w := &Worker{
		ID: "test-hybrid",
		Backend: &mockReadinessBackend{r: qmimanager.UIMReadiness{
			Reason: qmimanager.UIMReadinessReady,
			ICCID:  "stale-at-iccid",
			IMSI:   "stale-at-imsi",
		}},
		// An empty manager is intentionally not ready. If the auxiliary AT
		// readiness were still consulted first, this test would incorrectly pass.
		QMICore: &qmicore.Manager{},
	}
	p.mu.Lock()
	p.workers[w.ID] = w
	p.mu.Unlock()

	err := p.WaitQMICoreReady(w.ID, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected QMI readiness failure instead of stale AT success")
	}
	if !strings.Contains(err.Error(), "qmi_manager_not_available") {
		t.Fatalf("expected readiness result from QMI core, got %v", err)
	}
}
