//go:build !linux

package socketutil

// BindToDevice is a development-safe no-op on platforms that do not expose
// Linux SO_BINDTODEVICE. Production deployments continue to use the Linux
// implementation above.
func BindToDevice(_ uintptr, _ string) error {
	return nil
}
