package mux

import (
	"encoding/binary"
	"testing"
)

func TestParseControlFrame(t *testing.T) {
	frame := BuildControlFrame(42, ControlResetClient)

	control, ok := ParseControlFrame(frame)
	if !ok {
		t.Fatal("expected control frame")
	}
	if control.ClientID != 42 {
		t.Fatalf("ClientID = %d, want 42", control.ClientID)
	}
	if control.Type != ControlResetClient {
		t.Fatalf("Type = %d, want %d", control.Type, ControlResetClient)
	}
}

func TestHandleControlResetClient(t *testing.T) {
	m := New(0, func([]byte) error { return nil })

	dataFrame := make([]byte, 13)
	binary.BigEndian.PutUint32(dataFrame[0:4], 42)
	binary.BigEndian.PutUint16(dataFrame[4:6], 7)
	binary.BigEndian.PutUint16(dataFrame[6:8], 1)
	binary.BigEndian.PutUint32(dataFrame[8:12], 0)
	dataFrame[12] = 0xAA

	m.HandleFrame(dataFrame)
	if stream := m.GetStream(7); stream == nil {
		t.Fatal("expected data stream before reset")
	}

	m.HandleFrame(BuildControlFrame(42, ControlResetClient))
	if stream := m.GetStream(7); stream != nil {
		t.Fatal("expected data stream to be removed by client reset")
	}
}

func TestSendClientReset(t *testing.T) {
	var sent []byte
	m := New(99, func(frame []byte) error {
		sent = append([]byte(nil), frame...)
		return nil
	})

	if err := m.SendClientReset(); err != nil {
		t.Fatalf("SendClientReset failed: %v", err)
	}
	control, ok := ParseControlFrame(sent)
	if !ok {
		t.Fatal("expected sent control frame")
	}
	if control.ClientID != 99 || control.Type != ControlResetClient {
		t.Fatalf("control = %#v", control)
	}
}
