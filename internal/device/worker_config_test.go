package device

import (
	"strconv"
	"sync"
	"testing"

	"github.com/iniwex5/vohive/internal/config"
)

func TestWorkerConfigSnapshotConcurrentUpdates(t *testing.T) {
	w := &Worker{Config: config.DeviceConfig{ID: "dev-1", Name: "initial"}}
	const iterations = 1000

	var wg sync.WaitGroup
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				cfg := w.ConfigSnapshot()
				if cfg.ID != "dev-1" {
					t.Errorf("ConfigSnapshot().ID = %q", cfg.ID)
					return
				}
			}
		}()
	}
	for i := 0; i < iterations; i++ {
		name := "device-" + strconv.Itoa(i)
		w.updateConfig(func(cfg *config.DeviceConfig) { cfg.Name = name })
	}
	wg.Wait()
	if got := w.ConfigSnapshot().Name; got != "device-999" {
		t.Fatalf("ConfigSnapshot().Name = %q, want device-999", got)
	}
}
