//go:build !linux

package device

import "github.com/iniwex5/vohive/pkg/logger"

func (w *UdevWatcher) loop() {
	logger.Info("当前开发平台不支持 udev 热插拔监听，已跳过")
	<-w.stop
}
