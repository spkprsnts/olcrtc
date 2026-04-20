// Package client implements the local SOCKS5 client side of the olcrtc tunnel.
package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/mux"
	"github.com/openlibrecommunity/olcrtc/internal/names"
	"github.com/openlibrecommunity/olcrtc/internal/provider"
	"github.com/pion/webrtc/v4"
)

var (
	ErrKeySize         = errors.New("key must be 32 bytes")
	ErrKeyStringLength = errors.New("key string length must be 32")
	ErrInvalidSocks5   = errors.New("invalid SOCKS5 version")
	ErrNoPeers         = errors.New("no peers available")
	ErrEncryptFailed   = errors.New("encrypt failed")
)

// Client handles local SOCKS5 connections and tunnels them via WebRTC.
type Client struct {
	peers          []provider.Provider
	cipher         *crypto.Cipher
	mux            *mux.Multiplexer
	connections    map[uint16]net.Conn
	connMu         sync.RWMutex
	peerIdx        atomic.Uint32
	clientID       uint32
	activeClients  atomic.Int32
	wg             sync.WaitGroup
	dnsServer      string
}

// Run starts the client with the specified parameters.
func Run(
	ctx context.Context,
	providerName,
	roomURL,
	keyHex string,
	localAddr string,
	dnsServer,
	socksProxyAddr string,
	socksProxyPort int,
) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cipher, err := setupCipher(keyHex)
	if err != nil {
		return fmt.Errorf("setupCipher failed: %w", err)
	}

	clientIDBytes := make([]byte, 4)
	if _, err := rand.Read(clientIDBytes); err != nil {
		return fmt.Errorf("failed to generate client ID: %w", err)
	}
	clientID := binary.BigEndian.Uint32(clientIDBytes)

	c := &Client{
		cipher:      cipher,
		connections: make(map[uint16]net.Conn),
		peers:       make([]provider.Provider, 0),
		clientID:    clientID,
		dnsServer:   dnsServer,
	}

	c.setupMux()

	const peerCount = 1
	for i := range peerCount {
		if err := c.addPeer(runCtx, providerName, roomURL, i, cancel, dnsServer, socksProxyAddr, socksProxyPort); err != nil {
			return fmt.Errorf("addPeer failed: %w", err)
		}
	}

	ln, err := net.Listen("tcp", localAddr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}
	defer ln.Close()

	logger.Infof("SOCKS5 server listening on %s (ClientID: %d)", localAddr, clientID)

	go c.acceptLoop(runCtx, ln)

	<-runCtx.Done()
	c.shutdown()
	c.wg.Wait()

	return nil
}

func setupCipher(keyHex string) (*crypto.Cipher, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, ErrKeySize
	}

	keyStr := string(key)
	if len(keyStr) != 32 {
		return nil, ErrKeyStringLength
	}

	cipher, err := crypto.NewCipher(keyStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	return cipher, nil
}

func (c *Client) setupMux() {
	c.mux = mux.New(c.clientID, func(frame []byte) error {
		for {
			canSend := true
			for _, peer := range c.peers {
				if !peer.CanSend() {
					canSend = false
					break
				}
			}
			if canSend {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		encrypted, err := c.cipher.Encrypt(frame)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrEncryptFailed, err)
		}
		if len(c.peers) == 0 {
			return ErrNoPeers
		}
		idx := c.peerIdx.Add(1) % uint32(len(c.peers)) //nolint:gosec
		return c.peers[idx].Send(encrypted)
	})
}

func (c *Client) addPeer(
	ctx context.Context,
	providerName,
	roomURL string,
	peerID int,
	cancel context.CancelFunc,
	dnsServer,
	socksProxyAddr string,
	socksProxyPort int,
) error {
	peer, err := provider.New(ctx, providerName, provider.Config{
		RoomURL:   roomURL,
		Name:      names.Generate(),
		OnData:    c.onData,
		DNSServer: dnsServer,
		ProxyAddr: socksProxyAddr,
		ProxyPort: socksProxyPort,
	})
	if err != nil {
		return fmt.Errorf("failed to create peer: %w", err)
	}

	peer.SetEndedCallback(func(reason string) {
		logger.Infof("Client peer %d reported conference end: %s", peerID, reason)
		cancel()
	})
	c.peers = append(c.peers, peer)

	peer.SetReconnectCallback(func(dc *webrtc.DataChannel) {
		c.handlePeerReconnect(peerID, dc)
	})

	logger.Infof("Connecting peer %d to %s...", peerID, providerName)
	if err := peer.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect peer: %w", err)
	}
	logger.Infof("Peer %d connected", peerID)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		peer.WatchConnection(ctx)
	}()

	// Send initial reset to clean up any stale connections for this clientID on server
	if err := c.mux.SendClientReset(); err != nil {
		logger.Warnf("Failed to send initial client reset: %v", err)
	}

	return nil
}

func (c *Client) handlePeerReconnect(peerID int, dc *webrtc.DataChannel) {
	logger.Infof("peer %d reconnect event: dc=%v", peerID, dc != nil)

	c.connMu.Lock()
	for sid, conn := range c.connections {
		if conn != nil {
			_ = conn.Close()
		}
		delete(c.connections, sid)
	}
	c.connMu.Unlock()

	if dc != nil {
		c.mux.UpdateSendFunc(func(frame []byte) error {
			encrypted, err := c.cipher.Encrypt(frame)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrEncryptFailed, err)
			}
			if len(c.peers) == 0 {
				return ErrNoPeers
			}
			idx := c.peerIdx.Add(1) % uint32(len(c.peers)) //nolint:gosec
			return c.peers[idx].Send(encrypted)
		})
		c.mux.Reset()

		if err := c.mux.SendClientReset(); err != nil {
			logger.Warnf("Failed to send client reset after reconnect: %v", err)
		}
	}
}

func (c *Client) acceptLoop(ctx context.Context, ln net.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, err := ln.Accept()
			if err != nil {
				logger.Debugf("Accept error: %v", err)
				continue
			}
			go c.handleSOCKS5(ctx, conn)
		}
	}
}

func (c *Client) handleSOCKS5(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	if err := c.socks5Handshake(conn); err != nil {
		logger.Debugf("SOCKS5 handshake failed: %v", err)
		return
	}

	addr, port, err := c.socks5Request(conn)
	if err != nil {
		logger.Debugf("SOCKS5 request failed: %v", err)
		return
	}

	sid := c.mux.OpenStream()
	c.connMu.Lock()
	c.connections[sid] = conn
	c.connMu.Unlock()

	logger.Infof("sid=%d tunnel to %s:%d", sid, addr, port)

	req := map[string]any{
		"cmd":  "connect",
		"addr": addr,
		"port": port,
	}
	reqData, _ := json.Marshal(req)

	if err := c.mux.SendData(sid, reqData); err != nil {
		logger.Warnf("sid=%d send connect failed: %v", sid, err)
		return
	}

	dataReady := c.mux.WaitForData(sid)
	select {
	case <-dataReady:
		resp := c.mux.ReadStream(sid)
		if len(resp) > 0 && resp[0] == 0x00 {
			if _, err := conn.Write(replySuccess()); err != nil {
				return
			}
		} else {
			_, _ = conn.Write(replyHostUnreachable())
			return
		}
	case <-time.After(15 * time.Second):
		_, _ = conn.Write(replyHostUnreachable())
		c.mux.CleanupDataChannel(sid)
		return
	case <-ctx.Done():
		return
	}
	c.mux.CleanupDataChannel(sid)

	c.activeClients.Add(1)
	c.startStreamPump(ctx, sid, conn)
	c.pumpToMux(sid, conn)
}

func (c *Client) socks5Handshake(conn net.Conn) error {
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}

	if buf[0] != 5 {
		return ErrInvalidSocks5
	}

	methods := make([]byte, int(buf[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}

	_, err := conn.Write([]byte{5, 0})
	return err
}

func (c *Client) socks5Request(conn net.Conn) (string, int, error) {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", 0, err
	}

	if buf[0] != 5 || buf[1] != 1 {
		return "", 0, fmt.Errorf("unsupported SOCKS5 command: %d", buf[1])
	}

	var addr string
	switch buf[3] {
	case 1: // IPv4
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", 0, err
		}
		addr = net.IP(ip).String()
	case 3: // Domain
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", 0, err
		}
		domain := make([]byte, int(lenBuf[0]))
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", 0, err
		}
		addr = string(domain)
	case 4: // IPv6
		ip := make([]byte, 16)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", 0, err
		}
		addr = net.IP(ip).String()
	default:
		return "", 0, fmt.Errorf("unsupported address type: %d", buf[3])
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", 0, err
	}
	port := int(binary.BigEndian.Uint16(portBuf))

	return addr, port, nil
}

func (c *Client) onData(data []byte) {
	plaintext, err := c.cipher.Decrypt(data)
	if err != nil {
		logger.Debugf("Decrypt error: %v", err)
		return
	}

	c.mux.HandleFrame(plaintext)
}

func (c *Client) shutdown() {
	c.connMu.Lock()
	for _, conn := range c.connections {
		if conn != nil {
			_ = conn.Close()
		}
	}
	c.connMu.Unlock()

	for i, peer := range c.peers {
		logger.Infof("closing peer %d", i)
		_ = peer.Close()
	}
}

func (c *Client) pumpToMux(sid uint16, conn net.Conn) {
	defer func() {
		c.activeClients.Add(-1)
		_ = c.mux.CloseStream(sid)
		c.connMu.Lock()
		delete(c.connections, sid)
		c.connMu.Unlock()
	}()

	buf := make([]byte, 16384)
	totalSent := uint64(0)
	lastLog := time.Now()

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if totalSent > 1024*1024 {
				logger.Infof("sid=%d done total=%dMB", sid, totalSent/(1024*1024))
			}
			return
		}

		for !c.canSendData() {
			time.Sleep(20 * time.Millisecond)
		}

		if err := c.mux.SendData(sid, buf[:n]); err != nil {
			return
		}

		totalSent += uint64(n) //nolint:gosec
		if time.Since(lastLog) > 5*time.Second {
			logger.Infof("sid=%d sent=%dMB", sid, totalSent/(1024*1024))
			lastLog = time.Now()
		}
	}
}

func (c *Client) startStreamPump(ctx context.Context, sid uint16, conn net.Conn) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				data := c.mux.ReadStream(sid)
				if len(data) > 0 {
					if _, err := conn.Write(data); err != nil {
						_ = c.mux.CloseStream(sid)
						return
					}
				}
				if c.mux.StreamClosed(sid) {
					return
				}
			}
		}
	}()
}

func (c *Client) canSendData() bool {
	for _, peer := range c.peers {
		if !peer.CanSend() {
			return false
		}
	}
	return true
}

func replySuccess() []byte {
	return []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}
}

func replyHostUnreachable() []byte {
	return []byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0}
}
