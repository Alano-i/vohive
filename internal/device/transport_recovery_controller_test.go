package device

import (
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

func TestTransportRecoveryControllerSerializesPerDevice(t *testing.T) {
	controller := NewTransportRecoveryController(nil)
	event := TransportRecoveryEvent{
		DeviceID: "dev1",
		Kind:     TransportRecoveryEventRecoveryExhausted,
		Err:      errors.New("QMI: read failed: EOF"),
		At:       time.Now(),
	}

	if !controller.Observe(event) {
		t.Fatal("first Observe() = false, want true")
	}
	if controller.Observe(event) {
		t.Fatal("second Observe() = true, want false while recovery is active")
	}

	controller.Finish("dev1")
	if !controller.Observe(event) {
		t.Fatal("Observe() after Finish = false, want true")
	}
}

func TestTransportRecoveryDuplicateEventsDoNotConsumeBudget(t *testing.T) {
	controller := NewTransportRecoveryController(nil)
	event := TransportRecoveryEvent{DeviceID: "dev1", Kind: TransportRecoveryEventHealthSuspect}
	if accepted, overLimit := controller.ObserveWithBudget(event); !accepted || overLimit {
		t.Fatal("first event should be accepted")
	}
	for i := 0; i < rebuildMaxInWindow*2; i++ {
		if accepted, overLimit := controller.ObserveWithBudget(event); accepted || overLimit {
			t.Fatalf("active duplicate %d must be deduplicated without consuming budget", i+1)
		}
	}
	controller.Finish(event.DeviceID)
	for i := 1; i < rebuildMaxInWindow; i++ {
		if accepted, overLimit := controller.ObserveWithBudget(event); !accepted || overLimit {
			t.Fatalf("unique attempt %d should remain within budget", i+1)
		}
		controller.Finish(event.DeviceID)
	}
	if accepted, overLimit := controller.ObserveWithBudget(event); accepted || !overLimit {
		t.Fatal("next unique attempt should hit the rebuild budget")
	}
}

func TestTransportRecoveryControllerAllowsDifferentDevices(t *testing.T) {
	controller := NewTransportRecoveryController(nil)
	err := errors.New("QMI: read failed: EOF")

	if !controller.Observe(TransportRecoveryEvent{DeviceID: "dev1", Kind: TransportRecoveryEventRecoveryExhausted, Err: err}) {
		t.Fatal("dev1 Observe() = false, want true")
	}
	if !controller.Observe(TransportRecoveryEvent{DeviceID: "dev2", Kind: TransportRecoveryEventRecoveryExhausted, Err: err}) {
		t.Fatal("dev2 Observe() = false, want true")
	}
}

func TestTransportRecoveryControllerIgnoresStaleWorkerGeneration(t *testing.T) {
	controller := NewTransportRecoveryController(nil)
	controller.SetWorkerGenerationForTest("dev1", 3)

	if controller.Observe(TransportRecoveryEvent{
		DeviceID:         "dev1",
		WorkerGeneration: 2,
		Kind:             TransportRecoveryEventRecoveryExhausted,
		Err:              errors.New("QMI: read failed: EOF"),
	}) {
		t.Fatal("stale generation Observe() = true, want false")
	}
	if !controller.Observe(TransportRecoveryEvent{
		DeviceID:         "dev1",
		WorkerGeneration: 3,
		Kind:             TransportRecoveryEventRecoveryExhausted,
		Err:              errors.New("QMI: read failed: EOF"),
	}) {
		t.Fatal("current generation Observe() = false, want true")
	}
}

func TestTransportRecoveryControllerAcceptsStructuredRecoveryEvents(t *testing.T) {
	controller := NewTransportRecoveryController(nil)

	if !controller.Observe(TransportRecoveryEvent{
		DeviceID: "dev1",
		Kind:     TransportRecoveryEventRecoveryExhausted,
		Err:      errors.New("write failed: write unix @->@qmi-proxy: write: broken pipe"),
	}) {
		t.Fatal("recovery exhausted event Observe() = false, want true")
	}
	controller.Finish("dev1")
	if !controller.Observe(TransportRecoveryEvent{
		DeviceID: "dev1",
		Kind:     TransportRecoveryEventHealthSuspect,
		Err:      errors.New("QMI service operation timeout: NAS GetServingSystem: context deadline exceeded"),
	}) {
		t.Fatal("health threshold event Observe() = false, want true")
	}
}

func TestRemoveWorkerRegistrationIfCurrentKeepsNewWorker(t *testing.T) {
	pool := NewPool(&config.Config{})
	defer pool.cancel()

	oldWorker := &Worker{ID: "dev1", stop: make(chan struct{})}
	newWorker := &Worker{ID: "dev1", stop: make(chan struct{})}

	if err := pool.registerWorkerStarting(oldWorker); err != nil {
		t.Fatalf("register old worker: %v", err)
	}
	pool.mu.Lock()
	pool.workers["dev1"] = newWorker
	pool.mu.Unlock()

	pool.removeWorkerRegistrationIfCurrent(oldWorker)

	if got := pool.GetWorker("dev1"); got != newWorker {
		t.Fatalf("GetWorker() = %#v, want new worker", got)
	}
}

func TestQMIRecoveryActiveNotLeakedWhenModemRebootAlreadyRunning(t *testing.T) {
	pool := NewPool(&config.Config{})
	defer pool.cancel()
	pool.transportRecovery = NewTransportRecoveryController(pool)

	// Simulate an AT disconnect recovery occupying the modemRebootRecovering lock
	pool.beginModemRebootRecovery("dev1")

	pool.scheduleWorkerRecoveryWithTransportEvent("dev1", qmiTransportFailureRecoveryReason, &TransportRecoveryEvent{
		DeviceID: "dev1",
		Kind:     TransportRecoveryEventRecoveryExhausted,
		Source:   "recovery_exhausted:test",
		Err:      errors.New("qmi recovery exhausted"),
	})

	// Wait a brief moment to allow the goroutine to hit the beginModemRebootRecovery check and return
	time.Sleep(50 * time.Millisecond)

	// Verify that transportRecovery controller's active map is NOT occupied by this failed attempt
	pool.transportRecovery.mu.Lock()
	_, exists := pool.transportRecovery.active["dev1"]
	pool.transportRecovery.mu.Unlock()

	if exists {
		t.Fatal("transportRecovery.active leaked when modemRebootRecovery was already running")
	}
}

func TestObserveWithBudgetUsesSlidingWindow(t *testing.T) {
	c := NewTransportRecoveryController(nil)
	now := time.Now()
	dev := "dev-1"

	for i := 0; i < rebuildMaxInWindow; i++ {
		accepted, overLimit := c.ObserveWithBudget(TransportRecoveryEvent{
			DeviceID: dev, Kind: TransportRecoveryEventHealthSuspect,
			At: now.Add(time.Duration(i) * time.Minute),
		})
		if !accepted || overLimit {
			t.Fatalf("attempt %d within window should be allowed", i+1)
		}
		c.Finish(dev)
	}
	if accepted, overLimit := c.ObserveWithBudget(TransportRecoveryEvent{
		DeviceID: dev, Kind: TransportRecoveryEventHealthSuspect,
		At: now.Add(time.Duration(rebuildMaxInWindow) * time.Minute),
	}); accepted || !overLimit {
		t.Fatalf("attempt %d should be rejected (over window cap)", rebuildMaxInWindow+1)
	}
	if accepted, overLimit := c.ObserveWithBudget(TransportRecoveryEvent{
		DeviceID: dev, Kind: TransportRecoveryEventHealthSuspect,
		At: now.Add(rebuildWindow + time.Minute),
	}); !accepted || overLimit {
		t.Fatal("attempt after window should be allowed again")
	}
}

func TestObserveWithBudgetPersistsAcrossGenerationChange(t *testing.T) {
	c := NewTransportRecoveryController(nil)
	now := time.Now()
	dev := "dev-2"
	for i := 0; i < rebuildMaxInWindow; i++ {
		accepted, _ := c.ObserveWithBudget(TransportRecoveryEvent{
			DeviceID: dev, Kind: TransportRecoveryEventHealthSuspect, At: now,
		})
		if !accepted {
			t.Fatalf("attempt %d should be accepted", i+1)
		}
		c.Finish(dev)
	}
	c.SetWorkerGeneration(dev, 42)
	if accepted, overLimit := c.ObserveWithBudget(TransportRecoveryEvent{
		DeviceID: dev, WorkerGeneration: 42,
		Kind: TransportRecoveryEventHealthSuspect, At: now,
	}); accepted || !overLimit {
		t.Fatal("generation change must not reset the physical device rebuild budget")
	}
}
