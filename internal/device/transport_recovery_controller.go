package device

import (
	"strings"
	"sync"
	"time"
)

const (
	rebuildWindow      = 30 * time.Minute
	rebuildMaxInWindow = 5
)

type TransportRecoveryEventKind string

const (
	TransportRecoveryEventRecoveryExhausted TransportRecoveryEventKind = "recovery_exhausted"
	TransportRecoveryEventHealthSuspect     TransportRecoveryEventKind = "health_suspect"
	TransportRecoveryEventMissingWorker     TransportRecoveryEventKind = "missing_worker"
	TransportRecoveryEventManualReboot      TransportRecoveryEventKind = "manual_reboot"
	TransportRecoveryEventUdevWake          TransportRecoveryEventKind = "udev_wake"
)

type TransportRecoveryEvent struct {
	DeviceID         string
	WorkerGeneration uint64
	Kind             TransportRecoveryEventKind
	Source           string
	Err              error
	At               time.Time
}

type TransportRecoveryController struct {
	pool *Pool

	mu                sync.Mutex
	active            map[string]TransportRecoveryEvent
	workerGenerations map[string]uint64
	rebuildTimes      map[string][]time.Time
}

func NewTransportRecoveryController(pool *Pool) *TransportRecoveryController {
	return &TransportRecoveryController{
		pool:              pool,
		active:            make(map[string]TransportRecoveryEvent),
		workerGenerations: make(map[string]uint64),
		rebuildTimes:      make(map[string][]time.Time),
	}
}

func (c *TransportRecoveryController) Observe(event TransportRecoveryEvent) bool {
	accepted, _ := c.ObserveWithBudget(event)
	return accepted
}

// ObserveWithBudget atomically deduplicates a recovery and consumes one rebuild
// slot only when the event is actually accepted. This avoids duplicate QMI
// callbacks exhausting the budget without performing a rebuild.
func (c *TransportRecoveryController) ObserveWithBudget(event TransportRecoveryEvent) (accepted bool, overLimit bool) {
	if c == nil {
		return false, false
	}
	event.DeviceID = strings.TrimSpace(event.DeviceID)
	if event.DeviceID == "" || !event.startsRecovery() {
		return false, false
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if currentGeneration := c.workerGenerations[event.DeviceID]; currentGeneration != 0 && event.WorkerGeneration != 0 && event.WorkerGeneration != currentGeneration {
		return false, false
	}
	if _, exists := c.active[event.DeviceID]; exists {
		return false, false
	}
	cutoff := event.At.Add(-rebuildWindow)
	kept := c.rebuildTimes[event.DeviceID][:0]
	for _, ts := range c.rebuildTimes[event.DeviceID] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= rebuildMaxInWindow {
		c.rebuildTimes[event.DeviceID] = kept
		return false, true
	}
	c.rebuildTimes[event.DeviceID] = append(kept, event.At)
	c.active[event.DeviceID] = event
	return true, false
}

func (c *TransportRecoveryController) Finish(deviceID string) {
	if c == nil {
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return
	}
	c.mu.Lock()
	delete(c.active, deviceID)
	c.mu.Unlock()
}

func (c *TransportRecoveryController) SetWorkerGeneration(deviceID string, generation uint64) {
	if c == nil {
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return
	}
	c.mu.Lock()
	c.workerGenerations[deviceID] = generation
	c.mu.Unlock()
}

func (c *TransportRecoveryController) SetWorkerGenerationForTest(deviceID string, generation uint64) {
	c.SetWorkerGeneration(deviceID, generation)
}

func (event TransportRecoveryEvent) startsRecovery() bool {
	switch event.Kind {
	case TransportRecoveryEventRecoveryExhausted, TransportRecoveryEventHealthSuspect,
		TransportRecoveryEventMissingWorker, TransportRecoveryEventManualReboot, TransportRecoveryEventUdevWake:
		return true
	default:
		return false
	}
}
