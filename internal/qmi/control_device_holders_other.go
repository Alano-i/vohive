//go:build !linux

package qmicore

import "strings"

type qmiControlDeviceHolder struct {
	PID     int
	Command string
}

type qmiControlDeviceHolders struct {
	Holders []qmiControlDeviceHolder
	Unknown bool
}

func (h qmiControlDeviceHolders) onlyQMIProxy() bool {
	if len(h.Holders) == 0 {
		return false
	}
	for _, holder := range h.Holders {
		cmd := strings.ToLower(strings.TrimSpace(holder.Command))
		if !strings.Contains(cmd, "qmi-proxy") {
			return false
		}
	}
	return true
}

// Non-Linux development hosts do not expose /proc device holder metadata.
var detectQMIControlDeviceHolders = func(_ string) (qmiControlDeviceHolders, error) {
	return qmiControlDeviceHolders{Unknown: true}, nil
}
