package device

import (
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

// Over-cap MBIM exhausted events must NOT schedule a rebuild; instead the
// worker is marked Failed.
func TestMBIMRecoveryExhaustedRespectsRebuildGuard(t *testing.T) {
	p := NewPool(&config.Config{})
	defer p.cancel()
	worker := &Worker{ID: "mbim-dev", generation: 1}
	p.mu.Lock()
	p.workers[worker.ID] = worker
	p.mu.Unlock()
	p.transportRecovery.SetWorkerGeneration(worker.ID, 1)

	for i := 0; i < rebuildMaxInWindow; i++ {
		accepted, overLimit := p.transportRecovery.ObserveWithBudget(TransportRecoveryEvent{
			DeviceID: worker.ID, WorkerGeneration: 1,
			Kind: TransportRecoveryEventRecoveryExhausted, At: time.Now(),
		})
		if !accepted || overLimit {
			t.Fatalf("pre-fill attempt %d should be allowed", i+1)
		}
		p.transportRecovery.Finish(worker.ID)
	}

	scheduled := p.maybeScheduleTransportRebuild(worker, HealthLayerMBIM, "still_hung", errors.New("hung"))
	if scheduled {
		t.Fatal("rebuild should be refused once the window cap is hit")
	}
	if got := worker.HealthSnapshot().State; got != HealthStateFailed {
		t.Fatalf("worker state = %v, want Failed after guard refusal", got)
	}
}
