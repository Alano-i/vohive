package device

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

func validateVoWiFiDirectEPDG(ctx context.Context, epdg string) error {
	epdg = strings.TrimSpace(epdg)
	if epdg == "" {
		return fmt.Errorf("ePDG 地址为空")
	}
	host := epdg
	if splitHost, _, err := net.SplitHostPort(epdg); err == nil {
		host = splitHost
	}
	host = strings.Trim(host, "[]")
	if addr, err := netip.ParseAddr(host); err == nil {
		if isUnusableVoWiFiEPDGAddr(addr) {
			return fmt.Errorf("ePDG 地址不可用: %s", addr.String())
		}
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return fmt.Errorf("ePDG DNS 解析失败(%s): %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("ePDG DNS 未返回可用地址: %s", host)
	}
	unusable := 0
	seen := make([]string, 0, len(addrs))
	for _, item := range addrs {
		addr, ok := netip.AddrFromSlice(item.IP)
		if !ok {
			continue
		}
		seen = append(seen, addr.String())
		if isUnusableVoWiFiEPDGAddr(addr) {
			unusable++
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("ePDG DNS 未返回可用 IP: %s", host)
	}
	if unusable == len(seen) {
		return fmt.Errorf("ePDG DNS 解析到不可用地址: %s -> %s", host, strings.Join(seen, ", "))
	}
	return nil
}

func isUnusableVoWiFiEPDGAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	return addr.IsLoopback() || addr.IsUnspecified() || addr.IsMulticast()
}
