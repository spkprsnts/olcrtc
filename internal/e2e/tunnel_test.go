package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/app/session"
	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/client"
	"github.com/openlibrecommunity/olcrtc/internal/server"
)

const testKeyHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

type memorySession struct {
	stream *memoryStream
}

func (s *memorySession) Capabilities() carrier.Capabilities {
	return carrier.Capabilities{ByteStream: true}
}

func (s *memorySession) OpenByteStream() (carrier.ByteStream, error) {
	return s.stream, nil
}

type memoryRoom struct {
	mu      sync.Mutex
	streams map[*memoryStream]struct{}
}

type memoryStream struct {
	room   *memoryRoom
	onData func([]byte)

	mu        sync.Mutex
	connected bool
	closed    bool
}

func (s *memoryStream) Connect(context.Context) error {
	s.mu.Lock()
	s.connected = true
	s.mu.Unlock()
	return nil
}

func (s *memoryStream) Send(data []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return io.ErrClosedPipe
	}
	s.mu.Unlock()

	payload := append([]byte(nil), data...)
	s.room.mu.Lock()
	peers := make([]*memoryStream, 0, len(s.room.streams))
	for peer := range s.room.streams {
		if peer != s {
			peers = append(peers, peer)
		}
	}
	s.room.mu.Unlock()

	for _, peer := range peers {
		peer.deliver(payload)
	}
	return nil
}

func (s *memoryStream) deliver(data []byte) {
	s.mu.Lock()
	ready := s.connected && !s.closed && s.onData != nil
	onData := s.onData
	s.mu.Unlock()
	if ready {
		onData(append([]byte(nil), data...))
	}
}

func (s *memoryStream) Close() error {
	s.mu.Lock()
	s.closed = true
	s.connected = false
	s.mu.Unlock()
	return nil
}

func (s *memoryStream) SetReconnectCallback(func())    {}
func (s *memoryStream) SetShouldReconnect(func() bool) {}
func (s *memoryStream) SetEndedCallback(func(string))  {}
func (s *memoryStream) WatchConnection(ctx context.Context) {
	<-ctx.Done()
}
func (s *memoryStream) CanSend() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected && !s.closed
}

func registerMemoryCarrier(t *testing.T) string {
	t.Helper()
	session.RegisterDefaults()

	name := "e2e-memory-" + t.Name()
	room := &memoryRoom{streams: make(map[*memoryStream]struct{})}
	carrier.Register(name, func(_ context.Context, cfg carrier.Config) (carrier.Session, error) {
		stream := &memoryStream{room: room, onData: cfg.OnData}
		room.mu.Lock()
		room.streams[stream] = struct{}{}
		room.mu.Unlock()
		return &memorySession{stream: stream}, nil
	})
	return name
}

func startEchoServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	return ln.Addr().String()
}

func freeLocalAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local addr: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close reserved addr: %v", err)
	}
	return addr
}

func waitForReady(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not become ready")
	}
}

func TestClientServerSOCKSTunnelOverMemoryDatachannel(t *testing.T) {
	carrierName := registerMemoryCarrier(t)
	echoAddr := startEchoServer(t)
	socksAddr := freeLocalAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Run(
			ctx,
			"direct",
			"datachannel",
			carrierName,
			"room",
			testKeyHex,
			"client-1",
			"127.0.0.1:53",
			"",
			0,
			0,
			0,
			0,
			"",
			"",
			0,
			"",
			"",
			0,
			0,
			0,
			0,
		)
	}()

	ready := make(chan struct{})
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- client.RunWithReady(
			ctx,
			"direct",
			"datachannel",
			carrierName,
			"room",
			testKeyHex,
			"client-1",
			socksAddr,
			"127.0.0.1:53",
			"",
			"",
			func() { close(ready) },
			0,
			0,
			0,
			"",
			"",
			0,
			"",
			"",
			0,
			0,
			0,
			0,
		)
	}()
	waitForReady(t, ready)

	conn, err := net.DialTimeout("tcp4", socksAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial socks: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		t.Fatalf("write socks greeting: %v", err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		t.Fatalf("read socks greeting: %v", err)
	}
	if !bytes.Equal(greeting, []byte{5, 0}) {
		t.Fatalf("socks greeting = %v, want [5 0]", greeting)
	}

	host, portText, err := net.SplitHostPort(echoAddr)
	if err != nil {
		t.Fatalf("split echo addr: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("parse echo port: %v", err)
	}
	req := []byte{5, 1, 0, 1}
	req = append(req, net.ParseIP(host).To4()...)
	var portBuf [2]byte
	binary.BigEndian.PutUint16(portBuf[:], uint16(port))
	req = append(req, portBuf[:]...)
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write socks connect: %v", err)
	}

	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read socks connect reply: %v", err)
	}
	if !bytes.Equal(reply, []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("socks reply = %v, want success", reply)
	}

	payload := []byte("olcrtc-e2e-payload\n")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write tunneled payload: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read tunneled echo: %v", err)
	}
	if !bytes.Equal(line, payload) {
		t.Fatalf("echo = %q, want %q", line, payload)
	}

	cancel()
	for name, ch := range map[string]<-chan error{"client": clientErr, "server": serverErr} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("%s returned error: %v", name, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s did not stop", name)
		}
	}
}
