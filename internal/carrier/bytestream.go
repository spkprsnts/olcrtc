package carrier

import (
	"context"

	"github.com/openlibrecommunity/olcrtc/internal/provider"
	"github.com/pion/webrtc/v4"
)

// ByteStream is a carrier capability for bidirectional byte transport.
type ByteStream interface {
	Connect(ctx context.Context) error
	Send(data []byte) error
	Close() error
	SetReconnectCallback(cb func())
	SetShouldReconnect(fn func() bool)
	SetEndedCallback(cb func(string))
	WatchConnection(ctx context.Context)
	CanSend() bool
}

// VideoTrack is a carrier capability for bidirectional video transport.
type VideoTrack interface {
	Connect(ctx context.Context) error
	Close() error
	SetReconnectCallback(cb func())
	SetShouldReconnect(fn func() bool)
	SetEndedCallback(cb func(string))
	WatchConnection(ctx context.Context)
	CanSend() bool
	AddTrack(track webrtc.TrackLocal) error
	SetTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver))
}

type legacySession struct {
	provider provider.Provider
}

// Capabilities reports the transport primitives supported by the legacy carrier.
func (s *legacySession) Capabilities() Capabilities {
	caps := Capabilities{ByteStream: true}
	_, caps.VideoTrack = s.provider.(provider.VideoTrackCapable)
	return caps
}

// OpenByteStream adapts the legacy provider to a generic byte stream capability.
func (s *legacySession) OpenByteStream() (ByteStream, error) {
	return &legacyByteStream{provider: s.provider}, nil
}

// OpenVideoTrack adapts a legacy provider to the generic video track capability.
func (s *legacySession) OpenVideoTrack() (VideoTrack, error) {
	publisher, ok := s.provider.(provider.VideoTrackCapable)
	if !ok {
		return nil, ErrVideoTrackUnsupported
	}
	return &legacyVideoTrack{provider: publisher}, nil
}

type legacyByteStream struct {
	provider provider.Provider
}

func (p *legacyByteStream) Connect(ctx context.Context) error { return p.provider.Connect(ctx) }
func (p *legacyByteStream) Send(data []byte) error            { return p.provider.Send(data) }
func (p *legacyByteStream) Close() error                      { return p.provider.Close() }

func (p *legacyByteStream) SetReconnectCallback(cb func()) {
	p.provider.SetReconnectCallback(func(_ *webrtc.DataChannel) {
		if cb != nil {
			cb()
		}
	})
}

func (p *legacyByteStream) SetShouldReconnect(fn func() bool) { p.provider.SetShouldReconnect(fn) }
func (p *legacyByteStream) SetEndedCallback(cb func(string))  { p.provider.SetEndedCallback(cb) }
func (p *legacyByteStream) WatchConnection(ctx context.Context) {
	p.provider.WatchConnection(ctx)
}
func (p *legacyByteStream) CanSend() bool { return p.provider.CanSend() }

type legacyVideoTrack struct {
	provider provider.VideoTrackCapable
}

func (v *legacyVideoTrack) Connect(ctx context.Context) error {
	return v.provider.(provider.Provider).Connect(ctx)
}
func (v *legacyVideoTrack) Close() error { return v.provider.(provider.Provider).Close() }
func (v *legacyVideoTrack) SetShouldReconnect(fn func() bool) {
	v.provider.(provider.Provider).SetShouldReconnect(fn)
}
func (v *legacyVideoTrack) SetEndedCallback(cb func(string)) {
	v.provider.(provider.Provider).SetEndedCallback(cb)
}
func (v *legacyVideoTrack) WatchConnection(ctx context.Context) {
	v.provider.(provider.Provider).WatchConnection(ctx)
}
func (v *legacyVideoTrack) CanSend() bool { return v.provider.(provider.Provider).CanSend() }
func (v *legacyVideoTrack) AddTrack(track webrtc.TrackLocal) error {
	return v.provider.AddVideoTrack(track)
}
func (v *legacyVideoTrack) SetTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	v.provider.SetVideoTrackHandler(cb)
}
func (v *legacyVideoTrack) SetReconnectCallback(cb func()) {
	v.provider.(provider.Provider).SetReconnectCallback(func(_ *webrtc.DataChannel) {
		if cb != nil {
			cb()
		}
	})
}
