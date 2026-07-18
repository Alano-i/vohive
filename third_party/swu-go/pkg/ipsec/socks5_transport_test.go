package ipsec

import (
	"bytes"
	"testing"
)

func TestSocks5IKENonESPMarkerRoundTrip(t *testing.T) {
	ike := make([]byte, 28)
	ike[17] = 0x20

	marked := addNonESPMarker(ike)
	if len(marked) != len(ike)+4 {
		t.Fatalf("marked length = %d, want %d", len(marked), len(ike)+4)
	}
	if !bytes.Equal(marked[:4], []byte{0, 0, 0, 0}) {
		t.Fatalf("marker = %x, want 00000000", marked[:4])
	}
	if got := stripNonESPMarker(marked); !bytes.Equal(got, ike) {
		t.Fatalf("stripped packet differs from original IKE packet")
	}
}

func TestStripNonESPMarkerLeavesESPUnchanged(t *testing.T) {
	esp := []byte{0x12, 0x34, 0x56, 0x78, 0, 0, 0, 1}
	if got := stripNonESPMarker(esp); !bytes.Equal(got, esp) {
		t.Fatalf("ESP packet was modified: %x", got)
	}
}
