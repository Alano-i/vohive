//go:build linux

package modem

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// probeIMEIOnPort uses a genuinely non-blocking file descriptor on Linux.
// A tty can become readable and then have another process consume the bytes
// before read(2), which makes go.bug.st/serial's select-then-blocking-read path
// hang past its advertised timeout during device rescans.
func probeIMEIOnPort(atPort string, timeout time.Duration) (string, error) {
	fd, err := unix.Open(atPort, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)

	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return "", err
	}
	termios.Iflag = unix.IGNPAR
	termios.Oflag = 0
	termios.Lflag = 0
	termios.Cflag &^= unix.CSIZE | unix.PARENB | unix.CSTOPB | unix.CRTSCTS | unix.CBAUD
	termios.Cflag |= unix.CS8 | unix.CLOCAL | unix.CREAD | unix.B115200
	termios.Cc[unix.VMIN] = 0
	termios.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, termios); err != nil {
		return "", err
	}

	_, _ = unix.Write(fd, []byte("AT\r\n"))
	time.Sleep(40 * time.Millisecond)
	_, _ = unix.Write(fd, []byte("AT+CGSN\r\n"))

	deadline := time.Now().Add(timeout)
	buf := make([]byte, 1024)
	var acc strings.Builder
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		wait := 80 * time.Millisecond
		if remaining < wait {
			wait = remaining
		}
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLPRI}}
		ready, pollErr := unix.Poll(pollFDs, int((wait+time.Millisecond-1)/time.Millisecond))
		if pollErr == unix.EINTR {
			continue
		}
		if pollErr != nil {
			return "", pollErr
		}
		if ready == 0 {
			continue
		}
		revents := pollFDs[0].Revents
		if revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return "", fmt.Errorf("imei probe tty unavailable (poll revents=0x%x)", revents)
		}
		if revents&(unix.POLLIN|unix.POLLPRI) == 0 {
			continue
		}
		n, readErr := unix.Read(fd, buf)
		if readErr == unix.EINTR || readErr == unix.EAGAIN || readErr == unix.EWOULDBLOCK {
			continue
		}
		if readErr != nil {
			return "", readErr
		}
		if n > 0 {
			acc.Write(buf[:n])
			if imei := parseIMEI(acc.String()); imei != "" {
				return imei, nil
			}
		}
	}
	if imei := parseIMEI(acc.String()); imei != "" {
		return imei, nil
	}
	return "", errors.New("imei probe timeout")
}
