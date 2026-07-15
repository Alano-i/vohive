//go:build linux

package device

import (
	"errors"
	"time"

	"github.com/iniwex5/netlink/nl"
	"github.com/iniwex5/vohive/pkg/logger"
	"golang.org/x/sys/unix"
)

func (w *UdevWatcher) loop() {
	conn, err := nl.Subscribe(unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		logger.Warn("udev 监听器启动失败，热插拔功能不可用", "err", err)
		return
	}
	defer conn.Close()

	logger.Info("udev 设备热插拔监听器已启动")

	for {
		select {
		case <-w.stop:
			logger.Info("udev 监听器已停止")
			return
		default:
		}

		tv := unix.NsecToTimeval((1 * time.Second).Nanoseconds())
		_ = conn.SetReceiveTimeout(&tv)

		msgs, _, err := conn.Receive()
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				continue
			}
			continue
		}

		for _, msg := range msgs {
			if w.isModemEvent(msg.Data) {
				w.scheduleRescan()
				break
			}
		}
	}
}
