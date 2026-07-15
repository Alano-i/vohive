package modem

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInvalidateIMEIProbeCacheClearsHotplugIdentityHints(t *testing.T) {
	imeiCache.mu.Lock()
	imeiCache.m = map[string]imeiCacheItem{
		"/dev/ttyUSB8": {IMEI: "860000000000001", TS: time.Now(), Fingerprint: "old-device"},
	}
	imeiCache.mu.Unlock()
	t.Cleanup(InvalidateIMEIProbeCache)

	InvalidateIMEIProbeCache()

	imeiCache.mu.RLock()
	defer imeiCache.mu.RUnlock()
	if imeiCache.m != nil {
		t.Fatalf("cache=%v want nil after hotplug invalidation", imeiCache.m)
	}
}

func TestIMEIProbePortFingerprintChangesWhenDeviceNodeIsRecreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ttyUSB8")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := imeiProbePortFingerprint(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second-generation"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := imeiProbePortFingerprint(path)
	if first == second {
		t.Fatalf("fingerprint did not change after node recreation: %q", first)
	}
}
