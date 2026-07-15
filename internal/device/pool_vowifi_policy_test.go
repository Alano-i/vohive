package device

import (
	"errors"
	"testing"

	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

func TestShouldRetryVoWiFiAutoStart(t *testing.T) {
	if shouldRetryVoWiFiAutoStart(nil) {
		t.Fatalf("nil error should not enter retry path")
	}
	if !shouldRetryVoWiFiAutoStart(errors.New("temporary failure")) {
		t.Fatalf("non-policy error should keep retry behavior")
	}
	if shouldRetryVoWiFiAutoStart(carrier.NewVoWiFiBlockedMCCError("460")) {
		t.Fatalf("policy-blocked error should not retry")
	}
	if shouldRetryVoWiFiAutoStart(errors.New("ePDG DNS 解析到不可用地址: epdg.epc.mnc030.mcc621.pub.3gppnetwork.org -> 127.0.0.1")) {
		t.Fatalf("unusable ePDG DNS result should not retry")
	}
}
