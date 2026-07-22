package device

import "github.com/iniwex5/vohive/internal/config"

// ConfigSnapshot returns the current immutable runtime configuration. Worker.Config
// is the bootstrap snapshot and is never mutated after construction; updates are
// published atomically so background recovery and API readers cannot race.
func (w *Worker) ConfigSnapshot() config.DeviceConfig {
	if w == nil {
		return config.DeviceConfig{}
	}
	if current := w.configValue.Load(); current != nil {
		return *current
	}
	return w.Config
}

func (w *Worker) replaceConfig(cfg config.DeviceConfig) {
	if w == nil {
		return
	}
	next := cfg
	w.configValue.Store(&next)
}

func (w *Worker) updateConfig(update func(*config.DeviceConfig)) config.DeviceConfig {
	if w == nil {
		return config.DeviceConfig{}
	}
	for {
		current := w.configValue.Load()
		next := w.Config
		if current != nil {
			next = *current
		}
		update(&next)
		nextPtr := &next
		if w.configValue.CompareAndSwap(current, nextPtr) {
			return next
		}
	}
}

func (w *Worker) setRestoreNetworkAfterVoWiFi(enabled bool) {
	if w != nil {
		w.restoreNetworkAfterVoWiFi.Store(enabled)
	}
}

func (w *Worker) shouldRestoreNetworkAfterVoWiFi() bool {
	return w != nil && w.restoreNetworkAfterVoWiFi.Load()
}
