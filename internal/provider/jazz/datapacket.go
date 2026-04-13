package jazz

import (
	"encoding/binary"
	"io"

	"github.com/google/uuid"
)

func encodeVarint(value uint64) []byte {
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, value)
	return buf[:n]
}

func encodeField(fieldNumber int, wireType int, data []byte) []byte {
	tag := encodeVarint(uint64((fieldNumber << 3) | wireType))
	if wireType == 0 {
		return append(tag, data...)
	}
	if wireType == 2 {
		length := encodeVarint(uint64(len(data)))
		result := append(tag, length...)
		return append(result, data...)
	}
	return append(tag, data...)
}

func EncodeDataPacket(payload []byte) []byte {
	msgID := uuid.New().String()

	userFields := encodeField(2, 2, payload)
	userFields = append(userFields, encodeField(8, 2, []byte(msgID))...)

	userPacket := userFields

	dp := encodeField(1, 0, encodeVarint(0))
	dp = append(dp, encodeField(2, 2, userPacket)...)

	return dp
}

func readVarint(r io.ByteReader) (uint64, error) {
	return binary.ReadUvarint(r)
}

func DecodeDataPacket(raw []byte) ([]byte, bool) {
	reader := &byteReader{data: raw, pos: 0}

	var userData []byte

	for reader.pos < len(reader.data) {
		tagVal, err := readVarint(reader)
		if err != nil {
			break
		}

		fieldNumber := int(tagVal >> 3)
		wireType := int(tagVal & 0x07)

		if wireType == 0 {
			_, _ = readVarint(reader)
		} else if wireType == 2 {
			length, err := readVarint(reader)
			if err != nil {
				break
			}
			data := make([]byte, length)
			n, err := reader.Read(data)
			if err != nil || n != int(length) {
				break
			}
			if fieldNumber == 2 {
				userData = data
			}
		} else if wireType == 1 {
			reader.pos += 8
		} else if wireType == 5 {
			reader.pos += 4
		} else {
			break
		}
	}

	if userData == nil {
		return nil, false
	}

	innerReader := &byteReader{data: userData, pos: 0}
	var payload []byte

	for innerReader.pos < len(innerReader.data) {
		tagVal, err := readVarint(innerReader)
		if err != nil {
			break
		}

		fn := int(tagVal >> 3)
		wt := int(tagVal & 0x07)

		if wt == 0 {
			_, _ = readVarint(innerReader)
		} else if wt == 2 {
			length, err := readVarint(innerReader)
			if err != nil {
				break
			}
			data := make([]byte, length)
			n, err := innerReader.Read(data)
			if err != nil || n != int(length) {
				break
			}
			if fn == 2 {
				payload = data
			}
		} else if wt == 1 {
			innerReader.pos += 8
		} else if wt == 5 {
			innerReader.pos += 4
		} else {
			break
		}
	}

	return payload, len(payload) > 0
}

type byteReader struct {
	data []byte
	pos  int
}

func (b *byteReader) ReadByte() (byte, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	c := b.data[b.pos]
	b.pos++
	return c, nil
}

func (b *byteReader) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}
