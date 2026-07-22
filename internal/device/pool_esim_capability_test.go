package device

import (
	"errors"
	"testing"

	"github.com/iniwex5/vohive/internal/esim"
)

type esimCapabilityProbeStub struct {
	eids []esim.EUICCInfo
	err  error
}

func (s esimCapabilityProbeStub) GetEIDs() ([]esim.EUICCInfo, error) {
	return s.eids, s.err
}

func TestProbeESIMCapability(t *testing.T) {
	tests := []struct {
		name    string
		probe   esimCapabilityProbe
		want    esimCapabilityState
		wantErr bool
	}{
		{name: "missing probe", want: esimCapabilityUnknown},
		{name: "empty response is inconclusive", probe: esimCapabilityProbeStub{}, want: esimCapabilityUnknown},
		{name: "explicit no euicc", probe: esimCapabilityProbeStub{err: esim.ErrNoEUICC}, want: esimCapabilityUnsupported},
		{name: "euicc without profiles", probe: esimCapabilityProbeStub{eids: []esim.EUICCInfo{{EID: "89049032000001000000000000000001"}}}, want: esimCapabilitySupported},
		{name: "probe failure", probe: esimCapabilityProbeStub{err: errors.New("logical channel unavailable")}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := probeESIMCapability(tt.probe)
			if got != tt.want {
				t.Fatalf("probeESIMCapability()=%v want=%v", got, tt.want)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("probeESIMCapability() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestStaleESIMProbeCannotOverwriteNewSIMState(t *testing.T) {
	p := NewPool(nil)
	defer p.cancel()
	w := &Worker{ID: "dev-esim"}
	p.workers[w.ID] = w

	generation := w.esimGeneration.Load()
	p.resetESIMCapability(w, "sim_changed")
	if p.setESIMCapabilityForGeneration(w, esimCapabilitySupported, generation, "stale_probe") {
		t.Fatal("probe result from the previous SIM generation must be ignored")
	}
	if w.ESIMEnabled() {
		t.Fatal("stale probe re-enabled eSIM capability")
	}
}

func TestESIMCapabilityIsScopedToCurrentSIM(t *testing.T) {
	p := NewPool(nil)
	defer p.cancel()
	w := &Worker{ID: "dev-esim"}
	p.workers[w.ID] = w

	if !p.setESIMCapability(w, esimCapabilitySupported, "test_detected") || !w.ESIMEnabled() {
		t.Fatal("detected eUICC capability was not enabled")
	}
	if !p.resetESIMCapability(w, "sim_removed") || w.ESIMEnabled() {
		t.Fatal("removing the SIM must invalidate eSIM capability")
	}
	if !p.setESIMCapability(w, esimCapabilityUnsupported, "physical_sim") || w.ESIMEnabled() {
		t.Fatal("physical SIM must keep eSIM capability disabled")
	}
}
