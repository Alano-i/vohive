package carrier

import "testing"

func TestResolveEffectiveCarrierConfigUsesRuntimePresets(t *testing.T) {
	tests := []struct {
		name     string
		mcc      string
		mnc      string
		presetID string
		epdg     string
	}{
		{
			name:     "three hk",
			mcc:      "454",
			mnc:      "003",
			presetID: "three_hk_454003",
			epdg:     "wlan.three.com.hk",
		},
		{
			name:     "csl all zero mnc",
			mcc:      "454",
			mnc:      "000",
			presetID: "csl_454000",
			epdg:     "epdg.epc.mnc000.mcc454.pub.3gppnetwork.org",
		},
		{
			name:     "two degrees nz",
			mcc:      "530",
			mnc:      "24",
			presetID: "2degrees_nz_53024",
			epdg:     "epdg.ims.2degrees.net.nz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ResolveEffectiveCarrierConfig(EffectiveCarrierConfigInput{MCC: tt.mcc, MNC: tt.mnc})
			if cfg.PresetID != tt.presetID {
				t.Fatalf("PresetID = %q, want %q", cfg.PresetID, tt.presetID)
			}
			if cfg.EPDG.Host != tt.epdg {
				t.Fatalf("EPDG.Host = %q, want %q", cfg.EPDG.Host, tt.epdg)
			}
		})
	}
}
