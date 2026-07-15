//go:build !linux

package modem

import (
	"errors"
	"strings"
	"time"

	"go.bug.st/serial"
)

func probeIMEIOnPort(atPort string, timeout time.Duration) (string, error) {
	p, err := serial.Open(atPort, &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		Parity:   serial.NoParity,
	})
	if err != nil {
		return "", err
	}
	defer p.Close()
	_ = p.SetReadTimeout(80 * time.Millisecond)

	deadline := time.Now().Add(timeout)
	buf := make([]byte, 1024)
	var acc strings.Builder
	_, _ = p.Write([]byte("AT\r\n"))
	time.Sleep(40 * time.Millisecond)
	_, _ = p.Write([]byte("AT+CGSN\r\n"))
	for time.Now().Before(deadline) {
		n, readErr := p.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			if imei := parseIMEI(acc.String()); imei != "" {
				return imei, nil
			}
		}
		if readErr != nil && !strings.Contains(strings.ToLower(readErr.Error()), "timeout") {
			return "", readErr
		}
	}
	if imei := parseIMEI(acc.String()); imei != "" {
		return imei, nil
	}
	return "", errors.New("imei probe timeout")
}
