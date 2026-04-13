// Package provider defines the interface and registry for different WebRTC providers.
package provider

import (
	"context"

	"github.com/pion/webrtc/v4"
)

// Provider defines the standard interface for WebRTC connection handlers.
type Provider interface {
	Connect(ctx context.Context) error
	Send(data []byte) error
	Close() error
	SetReconnectCallback(cb func(*webrtc.DataChannel))
	SetShouldReconnect(fn func() bool)
	SetEndedCallback(cb func(string))
	WatchConnection(ctx context.Context)
	CanSend() bool
	GetSendQueue() chan []byte
	GetBufferedAmount() uint64
}

// Config holds common configuration for all providers.
type Config struct {
	RoomURL   string
	Name      string
	OnData    func([]byte)
	DNSServer string
	ProxyAddr string
	ProxyPort int
}

// Factory is a function that creates a new Provider instance.
type Factory func(ctx context.Context, cfg Config) (Provider, error)

//nolint:gochecknoglobals
var registry = make(map[string]Factory)

// Register adds a new provider factory to the registry.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// New creates a new Provider instance by name.
func New(ctx context.Context, name string, cfg Config) (Provider, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return factory(ctx, cfg)
}

// Available returns a list of registered provider names.
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
