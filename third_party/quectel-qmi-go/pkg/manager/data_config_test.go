package manager

import "testing"

func TestSetDataConfigUpdatesNextDialParameters(t *testing.T) {
	m := &Manager{cfg: Config{APN: "old.apn", EnableIPv4: true}}

	m.SetDataConfig(" club.apn ", false, true)

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg.APN != "club.apn" {
		t.Fatalf("APN=%q want club.apn", m.cfg.APN)
	}
	if m.cfg.EnableIPv4 || !m.cfg.EnableIPv6 {
		t.Fatalf("IP families v4=%v v6=%v want false/true", m.cfg.EnableIPv4, m.cfg.EnableIPv6)
	}
}
