package modem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// imeiCacheItem 存储 IMEI 缓存条目及对应的获取时间戳
type imeiCacheItem struct {
	IMEI        string
	TS          time.Time
	Fingerprint string
}

// imeiCache 提供线程安全的内存 IMEI 映射缓存，避免频繁通过串口发起硬件查询
var imeiCache struct {
	mu sync.RWMutex
	m  map[string]imeiCacheItem
}

// ProbeIMEICached 在 10 分钟缓存有效期内优先从内存缓存中获取指定 AT 串口的 IMEI；若未命中或过期，则调用底层串口方法探测
func ProbeIMEICached(atPort string, timeout time.Duration) (string, error) {
	atPort = strings.TrimSpace(atPort)
	if atPort == "" {
		return "", errors.New("empty at port")
	}

	fingerprint := imeiProbePortFingerprint(atPort)
	imeiCache.mu.RLock()
	if imeiCache.m != nil {
		if it, ok := imeiCache.m[atPort]; ok {
			if it.IMEI != "" && it.Fingerprint == fingerprint && time.Since(it.TS) < 10*time.Minute {
				imeiCache.mu.RUnlock()
				return it.IMEI, nil
			}
		}
	}
	imeiCache.mu.RUnlock()

	imei, err := ProbeIMEI(atPort, timeout)
	if err == nil && imei != "" {
		imeiCache.mu.Lock()
		if imeiCache.m == nil {
			imeiCache.m = make(map[string]imeiCacheItem)
		}
		imeiCache.m[atPort] = imeiCacheItem{IMEI: imei, TS: time.Now(), Fingerprint: fingerprint}
		imeiCache.mu.Unlock()
	}
	return imei, err
}

// InvalidateIMEIProbeCache clears tty-name based identity hints after any modem
// hotplug event. Linux may reuse ttyUSB numbers for a different USB interface,
// so carrying these hints across re-enumeration can bind a worker to a DIAG port.
func InvalidateIMEIProbeCache() {
	imeiCache.mu.Lock()
	imeiCache.m = nil
	imeiCache.mu.Unlock()
}

func imeiProbePortFingerprint(atPort string) string {
	parts := []string{strings.TrimSpace(atPort)}
	if resolved, err := filepath.EvalSymlinks(filepath.Join("/sys/class/tty", filepath.Base(atPort), "device")); err == nil {
		parts = append(parts, resolved)
	}
	if info, err := os.Stat(atPort); err == nil {
		parts = append(parts, fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size()))
	}
	return strings.Join(parts, "|")
}

// ProbeIMEI 通过打开底层 TTY 串口设备并执行 `AT+CGSN` 指令来实时探测模组的 IMEI 串号
func ProbeIMEI(atPort string, timeout time.Duration) (string, error) {
	atPort = strings.TrimSpace(atPort)
	if atPort == "" {
		return "", errors.New("empty at port")
	}
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}

	return probeIMEIOnPort(atPort, timeout)
}
