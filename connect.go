package e2b

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

const (
	connectFlagCompressed = 0x01
	connectFlagEndStream  = 0x02
)

// connectEnvelope wraps a JSON payload in the Connect streaming protocol
// envelope: 1 byte flags + 4 bytes big-endian message length + payload.
func connectEnvelope(payload []byte) ([]byte, error) {
	if len(payload) > math.MaxUint32 {
		return nil, fmt.Errorf("payload too large: %d bytes exceeds maximum frame size", len(payload))
	}
	var buf bytes.Buffer
	buf.WriteByte(0x00)
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(payload))); err != nil { // #nosec G115 -- bounds checked above
		return nil, fmt.Errorf("write envelope length: %w", err)
	}
	buf.Write(payload)
	return buf.Bytes(), nil
}

// connectFrame represents a single frame read from a Connect stream.
type connectFrame struct {
	Flags   byte
	Payload []byte
}

// isEndOfStream reports whether this frame is an end-of-stream trailer.
func (f *connectFrame) isEndOfStream() bool {
	return f.Flags&connectFlagEndStream != 0
}

// readConnectFrame reads a single Connect protocol frame from r.
// Returns io.EOF when no more frames are available.
func readConnectFrame(r io.Reader) (*connectFrame, error) {
	var flags byte
	if err := binary.Read(r, binary.BigEndian, &flags); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("read flags: %w", err)
	}

	var msgLen uint32
	if err := binary.Read(r, binary.BigEndian, &msgLen); err != nil {
		return nil, fmt.Errorf("read length: %w", err)
	}

	payload := make([]byte, msgLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}

	return &connectFrame{
		Flags:   flags,
		Payload: payload,
	}, nil
}

// connectTrailer is the JSON structure in an end-of-stream frame.
type connectTrailer struct {
	Error *connectError `json:"error,omitempty"`
}

// connectError is a Connect protocol error embedded in a trailer.
type connectError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// parseTrailerError checks if a trailer frame contains an error.
func parseTrailerError(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	var trailer connectTrailer
	if err := json.Unmarshal(payload, &trailer); err != nil {
		return nil
	}
	if trailer.Error != nil {
		return &Error{Message: fmt.Sprintf("connect %s: %s", trailer.Error.Code, trailer.Error.Message)}
	}
	return nil
}
