package videochannel

import (
	"encoding/binary"
	"fmt"
)

const (
	protocolMagic   uint32 = 0x4f565632 // OVV2
	protocolVersion byte   = 1
	frameTypeData   byte   = 1
	frameTypeAck    byte   = 2
)

type transportFrame struct {
	typ       byte
	seq       uint32
	crc       uint32
	totalLen  uint32
	fragIdx   uint16
	fragTotal uint16
	payload   []byte
}

type inboundMessage struct {
	totalLen uint32
	crc      uint32
	frags    [][]byte
	remain   int
}

func fragmentPayload(data []byte, maxSize int) [][]byte {
	if len(data) == 0 {
		return [][]byte{{}}
	}

	out := make([][]byte, 0, (len(data)+maxSize-1)/maxSize)
	for start := 0; start < len(data); start += maxSize {
		end := start + maxSize
		if end > len(data) {
			end = len(data)
		}

		chunk := make([]byte, end-start)
		copy(chunk, data[start:end])
		out = append(out, chunk)
	}

	return out
}

func encodeDataFrame(seq, crc uint32, totalLen, fragIdx, fragTotal int, payload []byte) []byte {
	out := make([]byte, 22+len(payload))
	binary.BigEndian.PutUint32(out[0:4], protocolMagic)
	out[4] = protocolVersion
	out[5] = frameTypeData
	binary.BigEndian.PutUint32(out[6:10], seq)
	binary.BigEndian.PutUint32(out[10:14], crc)
	binary.BigEndian.PutUint32(out[14:18], uint32(totalLen))
	binary.BigEndian.PutUint16(out[18:20], uint16(fragIdx))
	binary.BigEndian.PutUint16(out[20:22], uint16(fragTotal))
	copy(out[22:], payload)
	return out
}

func encodeAckFrame(seq, crc uint32) []byte {
	out := make([]byte, 14)
	binary.BigEndian.PutUint32(out[0:4], protocolMagic)
	out[4] = protocolVersion
	out[5] = frameTypeAck
	binary.BigEndian.PutUint32(out[6:10], seq)
	binary.BigEndian.PutUint32(out[10:14], crc)
	return out
}

func decodeTransportFrame(data []byte) (transportFrame, error) {
	if len(data) < 6 {
		return transportFrame{}, fmt.Errorf("frame too short")
	}
	if binary.BigEndian.Uint32(data[0:4]) != protocolMagic {
		return transportFrame{}, fmt.Errorf("unexpected frame magic")
	}
	if data[4] != protocolVersion {
		return transportFrame{}, fmt.Errorf("unexpected frame version")
	}

	frame := transportFrame{typ: data[5]}
	switch frame.typ {
	case frameTypeAck:
		if len(data) < 14 {
			return transportFrame{}, fmt.Errorf("ack too short")
		}
		frame.seq = binary.BigEndian.Uint32(data[6:10])
		frame.crc = binary.BigEndian.Uint32(data[10:14])
		return frame, nil
	case frameTypeData:
		if len(data) < 22 {
			return transportFrame{}, fmt.Errorf("data too short")
		}
		frame.seq = binary.BigEndian.Uint32(data[6:10])
		frame.crc = binary.BigEndian.Uint32(data[10:14])
		frame.totalLen = binary.BigEndian.Uint32(data[14:18])
		frame.fragIdx = binary.BigEndian.Uint16(data[18:20])
		frame.fragTotal = binary.BigEndian.Uint16(data[20:22])
		frame.payload = append([]byte(nil), data[22:]...)
		return frame, nil
	default:
		return transportFrame{}, fmt.Errorf("unexpected frame type")
	}
}
