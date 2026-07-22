package device

import (
	"errors"
	"time"

	"github.com/iniwex5/vohive/internal/esim"
	"github.com/iniwex5/vohive/pkg/logger"
)

const (
	esimCapabilityProbeAttempts = 3
	esimCapabilityProbeDelay    = 5 * time.Second
)

type esimCapabilityProbe interface {
	GetEIDs() ([]esim.EUICCInfo, error)
}

type esimCapabilityState uint32

const (
	esimCapabilityUnknown esimCapabilityState = iota
	esimCapabilitySupported
	esimCapabilityUnsupported
)

func probeESIMCapability(probe esimCapabilityProbe) (esimCapabilityState, error) {
	if probe == nil {
		return esimCapabilityUnknown, nil
	}
	eids, err := probe.GetEIDs()
	if err != nil {
		if errors.Is(err, esim.ErrNoEUICC) {
			return esimCapabilityUnsupported, nil
		}
		return esimCapabilityUnknown, err
	}
	if len(eids) == 0 {
		return esimCapabilityUnknown, nil
	}
	return esimCapabilitySupported, nil
}

func (p *Pool) discoverAndPrewarmESIMCapability(worker *Worker, reason string) esimCapabilityState {
	if p == nil || worker == nil {
		return esimCapabilityUnknown
	}
	worker.esimProbeMu.Lock()
	defer worker.esimProbeMu.Unlock()

	switch worker.esimCapabilityState() {
	case esimCapabilitySupported:
		p.prewarmActiveESIMProfileName(worker, reason)
		return esimCapabilitySupported
	case esimCapabilityUnsupported:
		return esimCapabilityUnsupported
	}
	generation := worker.esimGeneration.Load()

	if err := p.EnsureESIMRuntime(worker.ID, "esim_capability_probe"); err != nil {
		logger.Debug("eSIM 能力探测跳过：APDU 运行时不可用", "device", worker.ID, "reason", reason, "err", err)
		return esimCapabilityUnknown
	}
	if p.GetWorker(worker.ID) != worker || p.currentESIMCapabilityProbe(worker) == nil {
		return esimCapabilityUnknown
	}

	for attempt := 1; attempt <= esimCapabilityProbeAttempts; attempt++ {
		if worker.esimGeneration.Load() != generation {
			return esimCapabilityUnknown
		}
		probe := p.currentESIMCapabilityProbe(worker)
		if probe == nil {
			return esimCapabilityUnknown
		}
		state, err := probeESIMCapability(probe)
		if state != esimCapabilityUnknown {
			if !p.setESIMCapabilityForGeneration(worker, state, generation, reason) {
				if worker.EsimMgr != nil {
					worker.EsimMgr.InvalidateSIMCache()
				}
				return esimCapabilityUnknown
			}
			if state == esimCapabilitySupported {
				p.prewarmActiveESIMProfileName(worker, reason)
			} else {
				logger.Debug("当前 SIM 未发现 eUICC，按实体 SIM 处理", "device", worker.ID, "reason", reason)
			}
			return state
		}
		if err == nil || attempt == esimCapabilityProbeAttempts {
			logger.Debug("eSIM 能力探测未收敛，保持未知状态", "device", worker.ID, "reason", reason, "err", err)
			break
		}
		logger.Debug("eSIM 能力探测暂未成功，稍后重试", "device", worker.ID, "reason", reason, "attempt", attempt, "err", err)
		if !waitESIMCapabilityProbeRetry(p, worker, esimCapabilityProbeDelay) {
			return esimCapabilityUnknown
		}
	}
	return esimCapabilityUnknown
}

func (p *Pool) currentESIMCapabilityProbe(worker *Worker) esimCapabilityProbe {
	if p == nil || worker == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.workers[worker.ID] != worker {
		return nil
	}
	return worker.EsimMgr
}

func waitESIMCapabilityProbeRetry(p *Pool, worker *Worker, delay time.Duration) bool {
	if p == nil || worker == nil {
		return false
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-p.ctx.Done():
		return false
	case <-worker.stop:
		return false
	case <-timer.C:
		return p.isCurrentWorker(worker)
	}
}

func (w *Worker) esimCapabilityState() esimCapabilityState {
	if w == nil {
		return esimCapabilityUnknown
	}
	return esimCapabilityState(w.esimCapability.Load())
}

func (w *Worker) ESIMEnabled() bool {
	return w.esimCapabilityState() == esimCapabilitySupported
}

func (p *Pool) setESIMCapability(worker *Worker, state esimCapabilityState, reason string) bool {
	if worker == nil {
		return false
	}
	return p.setESIMCapabilityForGeneration(worker, state, worker.esimGeneration.Load(), reason)
}

func (p *Pool) setESIMCapabilityForGeneration(worker *Worker, state esimCapabilityState, generation uint64, reason string) bool {
	if p == nil || worker == nil || !p.isCurrentWorker(worker) {
		return false
	}
	if worker.esimGeneration.Load() != generation {
		return false
	}
	previous := esimCapabilityState(worker.esimCapability.Swap(uint32(state)))
	if previous == state {
		return false
	}
	logger.Info("当前 SIM 的 eSIM 能力状态已更新", "device", worker.ID, "state", state.String(), "reason", reason)
	p.broadcastVoWiFiStateChange(worker.ID)
	return true
}

func (p *Pool) MarkESIMSupported(deviceID, reason string) bool {
	return p.setESIMCapability(p.GetWorker(deviceID), esimCapabilitySupported, reason)
}

func (p *Pool) resetESIMCapability(worker *Worker, reason string) bool {
	if worker == nil {
		return false
	}
	generation := worker.esimGeneration.Add(1)
	if worker.EsimMgr != nil {
		worker.EsimMgr.InvalidateSIMCache()
	}
	return p.setESIMCapabilityForGeneration(worker, esimCapabilityUnknown, generation, reason)
}

func (s esimCapabilityState) String() string {
	switch s {
	case esimCapabilitySupported:
		return "supported"
	case esimCapabilityUnsupported:
		return "unsupported"
	default:
		return "unknown"
	}
}

var _ esimCapabilityProbe = (*esim.Manager)(nil)
