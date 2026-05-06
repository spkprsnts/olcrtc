package provider

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/pion/webrtc/v4"
)

type stubProvider struct{}

func (s *stubProvider) Connect(context.Context) error                               { return nil }
func (s *stubProvider) Send([]byte) error                                           { return nil }
func (s *stubProvider) Close() error                                                { return nil }
func (s *stubProvider) SetReconnectCallback(func(*webrtc.DataChannel))              {}
func (s *stubProvider) SetShouldReconnect(func() bool)                              {}
func (s *stubProvider) SetEndedCallback(func(string))                               {}
func (s *stubProvider) WatchConnection(context.Context)                             {}
func (s *stubProvider) CanSend() bool                                               { return true }
func (s *stubProvider) GetSendQueue() chan []byte                                   { return nil }
func (s *stubProvider) GetBufferedAmount() uint64                                   { return 0 }

func snapshotProviderRegistry() map[string]Factory {
	out := make(map[string]Factory, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

func restoreProviderRegistry(src map[string]Factory) {
	registry = make(map[string]Factory, len(src))
	for k, v := range src {
		registry[k] = v
	}
}

func TestNewAndAvailable(t *testing.T) {
	old := snapshotProviderRegistry()
	t.Cleanup(func() { restoreProviderRegistry(old) })

	called := false
	Register("test-provider", func(_ context.Context, cfg Config) (Provider, error) {
		called = cfg.Name == "peer"
		return &stubProvider{}, nil
	})

	got, err := New(context.Background(), "test-provider", Config{Name: "peer"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !called {
		t.Fatal("factory did not receive config")
	}
	if _, ok := got.(*stubProvider); !ok {
		t.Fatalf("New() returned %T, want *stubProvider", got)
	}

	if !reflect.DeepEqual(Available(), []string{"test-provider"}) {
		t.Fatalf("Available() = %#v, want %#v", Available(), []string{"test-provider"})
	}
}

func TestNewReturnsErrProviderNotFound(t *testing.T) {
	old := snapshotProviderRegistry()
	t.Cleanup(func() { restoreProviderRegistry(old) })
	registry = map[string]Factory{}

	_, err := New(context.Background(), "missing", Config{})
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("New() error = %v, want %v", err, ErrProviderNotFound)
	}
}
