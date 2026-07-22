package device

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

func TestRescanAndReconnectSerializesReconciliation(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("devices: []\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := config.InitGlobalManager(configPath); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}

	origDiscover := discoverQMIDevicesFn
	var active atomic.Int32
	var maxActive atomic.Int32
	discoverQMIDevicesFn = func() ([]QMIDevice, error) {
		current := active.Add(1)
		for {
			seen := maxActive.Load()
			if current <= seen || maxActive.CompareAndSwap(seen, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		active.Add(-1)
		return nil, nil
	}
	t.Cleanup(func() { discoverQMIDevicesFn = origDiscover })

	p := NewPool(&config.Config{})
	defer p.cancel()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.RescanAndReconnect(); err != nil {
				t.Errorf("RescanAndReconnect() error = %v", err)
			}
		}()
	}
	wg.Wait()
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("concurrent hardware reconciliations = %d, want 1", got)
	}
}

// 锁住现状:扫描到的硬件复用了某 IMEI 配置的旧路径,但实时 IMEI 与配置不符 → 不得绑定。
func TestRescanCharacterization_MismatchedIMEIOnReusedPathDoesNotBind(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	raw := "devices:\n- id: dev1\n  device_backend: qmi\n  modem_imei: \"111111111111111\"\n  control_device: /dev/cdc-wdm0\n  interface: wwan0\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := config.InitGlobalManager(configPath); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}

	origDiscover := discoverQMIDevicesFn
	discoverQMIDevicesFn = func() ([]QMIDevice, error) {
		return []QMIDevice{{ControlPath: "/dev/cdc-wdm0", NetInterface: "wwan0", USBPath: "/sys/bus/usb/devices/1-2"}}, nil
	}
	t.Cleanup(func() { discoverQMIDevicesFn = origDiscover })

	origResolve := resolveDiscoveredQMIDeviceFn
	resolveDiscoveredQMIDeviceFn = func(dev QMIDevice, timeout time.Duration, allowIMEIProbe bool) (QMIDevice, string) {
		// 返回一个不同的 IMEI
		return dev, "222222222222222"
	}
	t.Cleanup(func() { resolveDiscoveredQMIDeviceFn = origResolve })

	p := NewPool(&config.Config{})
	defer p.cancel()

	err := p.RescanAndReconnect()
	if err != nil {
		t.Fatalf("RescanAndReconnect failed: %v", err)
	}

	w := p.GetWorker("dev1")
	if w != nil {
		t.Fatalf("Expected no worker to be bound due to mismatched IMEI on reused path, got worker: %+v", w.Config)
	}
}

func TestTargetedRescanDoesNotRemoveMissingNonTargetWorker(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	raw := "devices:\n- id: healthy-peer\n  device_backend: qmi\n  modem_imei: \"111111111111111\"\n  control_device: /dev/cdc-wdm1\n  interface: wwan1\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := config.InitGlobalManager(configPath); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}

	origDiscover := discoverQMIDevicesFn
	discoverQMIDevicesFn = func() ([]QMIDevice, error) { return nil, nil }
	t.Cleanup(func() { discoverQMIDevicesFn = origDiscover })

	p := NewPool(&config.Config{})
	defer p.cancel()
	peer := &Worker{
		ID:     "healthy-peer",
		Config: config.DeviceConfig{ID: "healthy-peer", DeviceBackend: "qmi"},
		stop:   make(chan struct{}),
	}
	p.workers[peer.ID] = peer

	if err := p.rescanAndReconnect(rescanReconnectOptions{targetDeviceID: "recovering-device"}); err != nil {
		t.Fatalf("targeted rescan failed: %v", err)
	}
	if got := p.GetWorker(peer.ID); got != peer {
		t.Fatalf("non-target worker was removed or replaced: got=%p want=%p", got, peer)
	}
	select {
	case <-peer.stop:
		t.Fatal("non-target worker stop channel was closed")
	default:
	}
}
