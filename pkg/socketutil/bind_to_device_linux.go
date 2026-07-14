//go:build linux

package socketutil

import "syscall"

// BindToDevice pins a socket to a Linux network interface.
func BindToDevice(fd uintptr, iface string) error {
	return syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface)
}
