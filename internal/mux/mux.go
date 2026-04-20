// Package mux provides a multiplexer for multiple streams over a single connection.
package mux

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

var (
	// ErrClientResetID is returned when a client reset is attempted with a zero client ID.
	ErrClientResetID = errors.New("client reset requires a non-zero client id")
	// ErrDataTooLarge is returned when a data chunk exceeds the maximum frame size.
	ErrDataTooLarge = errors.New("data chunk too large")
)

const (
	// HeaderSize is the size of the frame header in bytes.
	HeaderSize = 12

	// ControlStreamID is a special stream ID used for control frames.
	ControlStreamID uint16 = 0xFFFF

	// ControlResetClient is a control frame type used to signal a client reset.
	ControlResetClient uint32 = 1

	// FrameTypeData is a marker for data frames.
	FrameTypeData uint16 = 0
	// FrameTypeControl is a marker for control frames.
	FrameTypeControl uint16 = 0xFFFF
)

// ControlFrame represents a control message between multiplexers.
type ControlFrame struct {
	ClientID uint32
	Type     uint32
}

// Stream represents a single multiplexed data stream.
type Stream struct {
	ID         uint16
	ClientID   uint32
	recvBuf    []byte
	closed     bool
	mu         sync.Mutex
	nextSeq    uint32
	outOfOrder map[uint32][]byte
}

// RecvBuf returns the current receive buffer content.
func (s *Stream) RecvBuf() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recvBuf
}

// Multiplexer coordinates multiple Streams over a single transport channel.
type Multiplexer struct {
	streams       map[uint16]*Stream
	nextID        uint16
	clientID      uint32
	onSend        func([]byte) error
	mu            sync.RWMutex
	maxStreams    int
	maxBufferSize int
	dataReady     map[uint16]chan struct{}
	dataReadyMu   sync.Mutex
	sendSeq       map[uint16]uint32
	sendSeqMu     sync.Mutex

	// bufferCond is used to wait for space in receive buffers
	bufferCond *sync.Cond
}

// New creates a new Multiplexer instance.
func New(clientID uint32, onSend func([]byte) error) *Multiplexer {
	m := &Multiplexer{
		streams:       make(map[uint16]*Stream),
		nextID:        1,
		clientID:      clientID,
		onSend:        onSend,
		maxStreams:    10000,
		maxBufferSize: 32 * 1024 * 1024,
		dataReady:     make(map[uint16]chan struct{}),
		sendSeq:       make(map[uint16]uint32),
	}
	m.bufferCond = sync.NewCond(&m.mu)
	return m
}

// OpenStream allocates and returns a new unique stream ID.
func (m *Multiplexer) OpenStream() uint16 {
	m.mu.Lock()
	defer m.mu.Unlock()

	for {
		sid := m.nextID
		m.nextID++
		if m.nextID == 0 {
			m.nextID = 1
		}

		if _, exists := m.streams[sid]; !exists {
			m.streams[sid] = &Stream{
				ID:         sid,
				recvBuf:    make([]byte, 0),
				nextSeq:    0,
				outOfOrder: make(map[uint32][]byte),
			}
			return sid
		}
	}
}

// SendData fragments and sends data over a specific stream.
func (m *Multiplexer) SendData(sid uint16, data []byte) error {
	m.mu.RLock()
	stream, exists := m.streams[sid]
	m.mu.RUnlock()

	if !exists || stream.closed {
		return nil
	}

	const chunkSize = 7000

	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}

		chunk := data[i:end]

		m.sendSeqMu.Lock()
		seq := m.sendSeq[sid]
		m.sendSeq[sid]++
		m.sendSeqMu.Unlock()

		if len(chunk) > math.MaxUint16 {
			return ErrDataTooLarge
		}

		frame := make([]byte, HeaderSize+len(chunk))
		binary.BigEndian.PutUint32(frame[0:4], m.clientID)
		binary.BigEndian.PutUint16(frame[4:6], sid)
		binary.BigEndian.PutUint16(frame[6:8], uint16(len(chunk))) //nolint:gosec // Length checked above
		binary.BigEndian.PutUint32(frame[8:12], seq)
		copy(frame[HeaderSize:], chunk)

		if err := m.onSend(frame); err != nil {
			return fmt.Errorf("onSend failed: %w", err)
		}
	}

	return nil
}

// CloseStream signals that a stream should be terminated.
func (m *Multiplexer) CloseStream(sid uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if stream, exists := m.streams[sid]; exists {
		stream.closed = true
	}

	m.sendSeqMu.Lock()
	delete(m.sendSeq, sid)
	m.sendSeqMu.Unlock()

	// Notify anyone waiting for buffer space that a stream is closed
	m.bufferCond.Broadcast()

	frame := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(frame[0:4], m.clientID)
	binary.BigEndian.PutUint16(frame[4:6], sid)
	binary.BigEndian.PutUint16(frame[6:8], 0)
	binary.BigEndian.PutUint32(frame[8:12], 0)

	if err := m.onSend(frame); err != nil {
		return fmt.Errorf("onSend failed: %w", err)
	}
	return nil
}

// SendClientReset sends a control frame to reset all streams for this client.
func (m *Multiplexer) SendClientReset() error {
	if m.clientID == 0 {
		return ErrClientResetID
	}
	if err := m.onSend(BuildControlFrame(m.clientID, ControlResetClient)); err != nil {
		return fmt.Errorf("onSend failed: %w", err)
	}
	return nil
}

// BuildControlFrame constructs a raw control frame.
func BuildControlFrame(clientID uint32, controlType uint32) []byte {
	frame := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(frame[0:4], clientID)
	binary.BigEndian.PutUint16(frame[4:6], ControlStreamID)
	binary.BigEndian.PutUint16(frame[6:8], 0xFFFF) // Use 0xFFFF as a marker for control
	binary.BigEndian.PutUint32(frame[8:12], controlType)
	return frame
}

// ParseControlFrame attempts to extract control information from a frame.
func ParseControlFrame(frame []byte) (ControlFrame, bool) {
	if len(frame) < HeaderSize {
		return ControlFrame{}, false
	}

	sid := binary.BigEndian.Uint16(frame[4:6])
	length := binary.BigEndian.Uint16(frame[6:8])
	if sid != ControlStreamID || length != 0xFFFF {
		return ControlFrame{}, false
	}

	return ControlFrame{
		ClientID: binary.BigEndian.Uint32(frame[0:4]),
		Type:     binary.BigEndian.Uint32(frame[8:12]),
	}, true
}

// HandleFrame processes an incoming frame from the transport.
func (m *Multiplexer) HandleFrame(frame []byte) {
	control, ok := ParseControlFrame(frame)
	if ok {
		m.handleControlFrame(control)
		return
	}

	if len(frame) < HeaderSize {
		return
	}

	clientID := binary.BigEndian.Uint32(frame[0:4])
	sid := binary.BigEndian.Uint16(frame[4:6])
	length := binary.BigEndian.Uint16(frame[6:8])
	seq := binary.BigEndian.Uint32(frame[8:12])

	if length == 0 {
		m.handleCloseStreamFrame(sid, clientID)
		return
	}

	if len(frame) < HeaderSize+int(length) {
		return
	}

	m.processDataFrame(sid, clientID, seq, frame[HeaderSize:HeaderSize+int(length)])
}

func (m *Multiplexer) handleCloseStreamFrame(sid uint16, clientID uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if stream, exists := m.streams[sid]; exists && stream.ClientID == clientID {
		stream.closed = true
		m.bufferCond.Broadcast()
	}
}

func (m *Multiplexer) processDataFrame(sid uint16, clientID uint32, seq uint32, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stream := m.getOrCreateStream(sid, clientID)
	if stream == nil {
		return
	}

	if seq == stream.nextSeq {
		if s := m.waitForBufferSpace(sid, clientID, len(data)); s != nil {
			s.recvBuf = append(s.recvBuf, data...)
			s.nextSeq++
			m.applyOutOfOrder(s, sid, clientID)
			m.notifyDataReady(sid)
		}
	} else if seq > stream.nextSeq {
		if len(stream.outOfOrder) < 100 {
			stream.outOfOrder[seq] = append([]byte(nil), data...)
		}
	}
}

func (m *Multiplexer) getOrCreateStream(sid uint16, clientID uint32) *Stream {
	stream, exists := m.streams[sid]
	if !exists {
		if len(m.streams) >= m.maxStreams {
			return nil
		}
		stream = &Stream{
			ID:         sid,
			ClientID:   clientID,
			recvBuf:    make([]byte, 0),
			nextSeq:    0,
			outOfOrder: make(map[uint32][]byte),
		}
		m.streams[sid] = stream
		return stream
	}

	if stream.ClientID != clientID {
		stream.ClientID = clientID
		stream.recvBuf = make([]byte, 0)
		stream.closed = false
		stream.nextSeq = 0
		stream.outOfOrder = make(map[uint32][]byte)
		m.bufferCond.Broadcast()
	}
	return stream
}

func (m *Multiplexer) applyOutOfOrder(stream *Stream, sid uint16, clientID uint32) {
	for {
		nextData, ok := stream.outOfOrder[stream.nextSeq]
		if !ok {
			break
		}
		if s := m.waitForBufferSpace(sid, clientID, len(nextData)); s == nil {
			return
		}
		stream.recvBuf = append(stream.recvBuf, nextData...)
		delete(stream.outOfOrder, stream.nextSeq)
		stream.nextSeq++
		logger.Verbosef("Applied out-of-order packet sid=%d seq=%d", sid, stream.nextSeq-1)
	}
}

func (m *Multiplexer) notifyDataReady(sid uint16) {
	m.dataReadyMu.Lock()
	defer m.dataReadyMu.Unlock()
	if ch, ok := m.dataReady[sid]; ok {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (m *Multiplexer) handleControlFrame(control ControlFrame) {
	switch control.Type {
	case ControlResetClient:
		m.ResetClient(control.ClientID)
	default:
		logger.Debugf("Unknown mux control frame type=%d clientID=%d", control.Type, control.ClientID)
	}
}

// ResetClient closes and removes all streams associated with a client ID.
func (m *Multiplexer) ResetClient(clientID uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for streamSid, stream := range m.streams {
		if stream.ClientID == clientID {
			stream.closed = true
			delete(m.streams, streamSid)
		}
	}
	m.bufferCond.Broadcast()
}

func (m *Multiplexer) waitForBufferSpace(sid uint16, clientID uint32, need int) *Stream {
	for {
		stream, ok := m.streams[sid]
		if !ok || stream.ClientID != clientID || stream.closed {
			return nil
		}
		if len(stream.recvBuf)+need <= m.maxBufferSize {
			return stream
		}
		// Wait for space to become available
		m.bufferCond.Wait()
	}
}

// ReadStream retrieves and clears the current receive buffer for a stream.
func (m *Multiplexer) ReadStream(sid uint16) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	stream, exists := m.streams[sid]
	if !exists || len(stream.recvBuf) == 0 {
		return nil
	}

	data := stream.recvBuf
	stream.recvBuf = make([]byte, 0)

	// Notify producers that space is now available
	m.bufferCond.Broadcast()

	return data
}

// StreamClosed returns true if the stream is closed or doesn't exist.
func (m *Multiplexer) StreamClosed(sid uint16) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stream, exists := m.streams[sid]
	return !exists || stream.closed
}

// GetStreams returns a list of all active stream IDs.
func (m *Multiplexer) GetStreams() []uint16 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sids := make([]uint16, 0, len(m.streams))
	for sid := range m.streams {
		sids = append(sids, sid)
	}
	return sids
}

// GetStream returns the Stream object for a given ID.
func (m *Multiplexer) GetStream(sid uint16) *Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.streams[sid]
}

// Reset clears all multiplexer state and closes all streams.
func (m *Multiplexer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, stream := range m.streams {
		stream.closed = true
	}

	m.streams = make(map[uint16]*Stream)
	m.nextID = 1

	m.sendSeqMu.Lock()
	m.sendSeq = make(map[uint16]uint32)
	m.sendSeqMu.Unlock()

	m.bufferCond.Broadcast()
}

// UpdateSendFunc updates the function used to transmit raw frames.
func (m *Multiplexer) UpdateSendFunc(onSend func([]byte) error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.onSend = onSend
}

// WaitForData returns a channel that signals when new data is available for a stream.
func (m *Multiplexer) WaitForData(sid uint16) <-chan struct{} {
	m.dataReadyMu.Lock()
	defer m.dataReadyMu.Unlock()

	if _, ok := m.dataReady[sid]; !ok {
		m.dataReady[sid] = make(chan struct{}, 1)
	}
	return m.dataReady[sid]
}

// CleanupDataChannel removes the data notification channel for a stream.
func (m *Multiplexer) CleanupDataChannel(sid uint16) {
	m.dataReadyMu.Lock()
	defer m.dataReadyMu.Unlock()

	if ch, ok := m.dataReady[sid]; ok {
		close(ch)
		delete(m.dataReady, sid)
	}
}
