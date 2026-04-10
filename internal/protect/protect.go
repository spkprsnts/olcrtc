package protect

import (
	"context"
	"net"
	"net/http"
	"syscall"
	"time"
)

// Protector is called with a socket file descriptor before connect.
// On Android, this calls VpnService.protect(fd) to bypass VPN routing.
var Protector func(fd int) bool

func controlFunc(network, address string, c syscall.RawConn) error {
	if Protector == nil {
		return nil
	}
	var err error
	c.Control(func(fd uintptr) {
		if !Protector(int(fd)) {
			err = &net.OpError{Op: "protect", Net: network, Err: net.ErrClosed}
		}
	})
	return err
}

// NewDialer returns a net.Dialer that calls Protector on each new socket.
func NewDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   controlFunc,
	}
}

// NewHTTPClient returns an http.Client using protected sockets.
func NewHTTPClient() *http.Client {
	dialer := NewDialer()
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:  10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	return &http.Client{Transport: transport}
}

// DialContext dials using a protected socket.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return NewDialer().DialContext(ctx, network, address)
}

// proxyDialer implements golang.org/x/net/proxy.Dialer for pion ICE.
type proxyDialer struct{}

func (d *proxyDialer) Dial(network, addr string) (net.Conn, error) {
	return NewDialer().Dial(network, addr)
}

// NewProxyDialer returns a proxy.Dialer that protects ICE sockets.
func NewProxyDialer() *proxyDialer {
	return &proxyDialer{}
}
