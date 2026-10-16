package vela

import (
	"bufio"
	"encoding/binary"
	"io"
)

// Framing is separate from the raw codec: [uvarint frame length][version 1][payload].
const FrameMaxDefault = 16 << 20

func AppendFrame(dst, payload []byte) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(payload)+1))
	dst = append(dst, 1)
	return append(dst, payload...)
}
func ReadFrame(br *bufio.Reader, maxBytes uint64) ([]byte, error) {
	if maxBytes == 0 {
		maxBytes = FrameMaxDefault
	}
	var n uint64
	var raw [10]byte
	var err error
	for i := 0; i < len(raw); i++ {
		raw[i], err = br.ReadByte()
		if err != nil {
			return nil, err
		}
		if raw[i] < 0x80 {
			k := i + 1
			decoded, count := binary.Uvarint(raw[:k])
			if count != k || (k > 1 && raw[i] == 0) {
				return nil, ErrInvalid
			}
			n = decoded
			break
		}
		if i == 9 {
			return nil, ErrInvalid
		}
	}

	if n == 0 || n > maxBytes || n > uint64(int(^uint(0)>>1)) {
		return nil, ErrLimit
	}
	b := make([]byte, int(n))
	if _, err = io.ReadFull(br, b); err != nil {
		return nil, err
	}
	if b[0] != 1 {
		return nil, ErrInvalid
	}
	return b[1:], nil
}

// WriteFrame limits serialized size and checks for short writes.
func WriteFrame(w io.Writer, payload []byte, maxBytes uint64) (int64, error) {
	if maxBytes == 0 {
		maxBytes = FrameMaxDefault
	}
	if uint64(len(payload)) >= maxBytes {
		return 0, ErrLimit
	}
	data := AppendFrame(nil, payload)
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}

// Reader maximum includes the one-byte frame version, not the length prefix.
