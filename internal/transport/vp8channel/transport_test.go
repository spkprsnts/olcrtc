package vp8channel

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeDataFrame(t *testing.T) {
	testCases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello")},
		{"medium", bytes.Repeat([]byte("x"), 1000)},
		{"large", bytes.Repeat([]byte("y"), 50000)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeDataFrame(tc.data)

			if encoded[0] != dataMarker {
				t.Errorf("expected marker 0x%02x, got 0x%02x", dataMarker, encoded[0])
			}

			decoded := extractDataFromPayload(encoded)
			if decoded == nil {
				t.Fatal("extractDataFromPayload returned nil")
			}

			if !bytes.Equal(decoded, tc.data) {
				t.Errorf("data mismatch: got %d bytes, want %d bytes", len(decoded), len(tc.data))
			}
		})
	}
}

func TestExtractDataFromPayload_Invalid(t *testing.T) {
	testCases := []struct {
		name  string
		frame []byte
	}{
		{"too short", []byte{0xFF, 0x00}},
		{"wrong marker", []byte{0x9D, 0x01, 0x2A, 0x00, 0x00}},
		{"length mismatch", []byte{0xFF, 0x00, 0x00, 0x00, 0x10, 0x01, 0x02}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := extractDataFromPayload(tc.frame)
			if result != nil {
				t.Errorf("expected nil, got %v", result)
			}
		})
	}
}

func TestExtractDataFromPayload_Keepalive(t *testing.T) {
	result := extractDataFromPayload(vp8Keepalive)
	if result != nil {
		t.Errorf("keepalive should return nil, got %v", result)
	}
}

func TestVP8KeepaliveFormat(t *testing.T) {
	if len(vp8Keepalive) < 3 {
		t.Fatal("keepalive too short")
	}

	if vp8Keepalive[0] == dataMarker {
		t.Error("keepalive should not start with data marker")
	}
}
