package videochannel

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestFragmentPayload(t *testing.T) {
	frags := fragmentPayload([]byte("abcdef"), 2)
	want := [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}
	if len(frags) != len(want) {
		t.Fatalf("fragment count = %d, want %d", len(frags), len(want))
	}
	for i := range frags {
		if !bytes.Equal(frags[i], want[i]) {
			t.Fatalf("frag %d = %q, want %q", i, frags[i], want[i])
		}
	}

	empty := fragmentPayload(nil, 10)
	if len(empty) != 1 || len(empty[0]) != 0 {
		t.Fatalf("fragmentPayload(nil) = %#v, want one empty frag", empty)
	}
}

func TestDecodeTransportFrameErrorsAndAck(t *testing.T) {
	tests := []struct {
		data []byte
		want error
	}{
		{data: []byte{1, 2, 3}, want: ErrFrameTooShort},
		{data: []byte{0, 0, 0, 0, protocolVersion, frameTypeAck}, want: ErrUnexpectedMagic},
		{data: []byte{0x4f, 0x56, 0x56, 0x32, 9, frameTypeAck}, want: ErrUnexpectedVersion},
		{data: []byte{0x4f, 0x56, 0x56, 0x32, protocolVersion, frameTypeAck}, want: ErrAckTooShort},
		{data: []byte{0x4f, 0x56, 0x56, 0x32, protocolVersion, frameTypeData}, want: ErrDataTooShort},
		{data: []byte{0x4f, 0x56, 0x56, 0x32, protocolVersion, 99}, want: ErrUnexpectedFrameType},
	}
	for _, tt := range tests {
		if _, err := decodeTransportFrame(tt.data); !errors.Is(err, tt.want) {
			t.Fatalf("decodeTransportFrame(%v) error = %v, want %v", tt.data, err, tt.want)
		}
	}

	ack, err := decodeTransportFrame(encodeAckFrame(7, 0x1234))
	if err != nil {
		t.Fatalf("decode ack error = %v", err)
	}
	if ack.typ != frameTypeAck || ack.seq != 7 || ack.crc != 0x1234 {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestCodecSpecsAndArgs(t *testing.T) {
	for _, mime := range []string{webrtc.MimeTypeH264, webrtc.MimeTypeVP8, webrtc.MimeTypeVP9} {
		spec, ok := codecSpecForMime(mime)
		if !ok {
			t.Fatalf("codecSpecForMime(%q) ok = false", mime)
		}
		if spec.mimeType != mime || spec.depacketizer == nil || spec.capability.ClockRate != 90000 {
			t.Fatalf("codec spec = %+v", spec)
		}
	}
	if _, ok := codecSpecForMime("video/unknown"); ok {
		t.Fatal("codecSpecForMime() accepted unknown mime")
	}

	if got := resolveEncoderCodec(h264CodecSpec(), "nvenc"); got != "h264_nvenc" {
		t.Fatalf("resolveEncoderCodec(h264,nvenc) = %q", got)
	}
	if got := resolveEncoderCodec(vp8CodecSpec(), "none"); got != "libvpx" {
		t.Fatalf("resolveEncoderCodec(vp8,none) = %q", got)
	}

	args := buildEncoderArgs(vp8CodecSpec(), "vp8_nvenc", 320, 240, 30, "1M")
	for _, want := range []string{"-video_size", "320x240", "-framerate", "30", "vp8_nvenc", "-b:v", "1M", "ivf"} {
		if !slices.Contains(args, want) {
			t.Fatalf("buildEncoderArgs() = %v, missing %q", args, want)
		}
	}
	h264Args := buildEncoderArgs(h264CodecSpec(), "libx264", 320, 240, 30, "1M")
	if h264Args[len(h264Args)-2] != "h264" {
		t.Fatalf("h264 encoder args = %v", h264Args)
	}
}

type shortWriter struct {
	writes int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		return 1, nil
	}
	return len(p), nil
}

type errWriter struct{}

func (w errWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestIVFWritersAndWithStderr(t *testing.T) {
	var buf bytes.Buffer
	if err := writeIVFHeader(&buf, "VP80", 320, 240, 30); err != nil {
		t.Fatalf("writeIVFHeader() error = %v", err)
	}
	if buf.Len() != 32 || string(buf.Bytes()[:4]) != "DKIF" {
		t.Fatalf("IVF header = %v", buf.Bytes())
	}

	buf.Reset()
	if err := writeIVFFrame(&buf, 3, []byte("abc")); err != nil {
		t.Fatalf("writeIVFFrame() error = %v", err)
	}
	if buf.Len() != 15 {
		t.Fatalf("IVF frame len = %d, want 15", buf.Len())
	}

	if err := writeAll(&shortWriter{}, []byte("abc")); err != nil {
		t.Fatalf("writeAll(shortWriter) error = %v", err)
	}
	if err := writeAll(errWriter{}, []byte("abc")); err == nil || !strings.Contains(err.Error(), "write:") {
		t.Fatalf("writeAll(errWriter) error = %v", err)
	}

	baseErr := errors.New("base")
	if got := withStderr(baseErr, bytes.NewBufferString(" details \n")); got == nil || got.Error() != "base: details" {
		t.Fatalf("withStderr() = %v", got)
	}
	if got := withStderr(nil, bytes.NewBufferString("details")); got != nil {
		t.Fatalf("withStderr(nil) = %v", got)
	}
}
