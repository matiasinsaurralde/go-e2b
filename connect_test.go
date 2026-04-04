package e2b

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestConnectEnvelope(t *testing.T) {
	payload := []byte(`{"hello":"world"}`)
	envelope, err := connectEnvelope(payload)
	if err != nil {
		t.Fatalf("connectEnvelope: %v", err)
	}

	// Verify structure: 1 byte flags + 4 bytes length + payload.
	if len(envelope) != 1+4+len(payload) {
		t.Fatalf("envelope length = %d, want %d", len(envelope), 1+4+len(payload))
	}

	if envelope[0] != 0x00 {
		t.Errorf("flags = %x, want 0x00", envelope[0])
	}

	msgLen := binary.BigEndian.Uint32(envelope[1:5])
	if int(msgLen) != len(payload) {
		t.Errorf("encoded length = %d, want %d", msgLen, len(payload))
	}

	if !bytes.Equal(envelope[5:], payload) {
		t.Errorf("payload mismatch")
	}
}

func TestReadConnectFrame(t *testing.T) {
	payload := []byte(`{"event":{}}`)
	data := makeFrame(0x00, payload)

	frame, err := readConnectFrame(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("readConnectFrame: %v", err)
	}

	if frame.Flags != 0x00 {
		t.Errorf("flags = %x, want 0x00", frame.Flags)
	}

	if !bytes.Equal(frame.Payload, payload) {
		t.Errorf("payload mismatch")
	}

	if frame.isEndOfStream() {
		t.Error("unexpected end of stream")
	}
}

func TestReadConnectFrameEndOfStream(t *testing.T) {
	data := makeFrame(connectFlagEndStream, nil)

	frame, err := readConnectFrame(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("readConnectFrame: %v", err)
	}

	if !frame.isEndOfStream() {
		t.Error("expected end of stream")
	}
}

func TestReadConnectFrameEOF(t *testing.T) {
	_, err := readConnectFrame(bytes.NewReader(nil))
	if err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestParseTrailerErrorNil(t *testing.T) {
	if err := parseTrailerError(nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := parseTrailerError([]byte(`{}`)); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseTrailerErrorPresent(t *testing.T) {
	payload := []byte(`{"error":{"code":"deadline_exceeded","message":"timeout"}}`)
	err := parseTrailerError(payload)
	if err == nil {
		t.Fatal("expected error")
	}

	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if e.StatusCode != 0 {
		t.Errorf("status code = %d, want 0", e.StatusCode)
	}
}

func TestReadConnectFrameTruncatedLength(t *testing.T) {
	// Only flags byte, no length — triggers read length error.
	_, err := readConnectFrame(bytes.NewReader([]byte{0x00}))
	if err == nil {
		t.Fatal("expected error for truncated length")
	}
}

func TestReadConnectFrameTruncatedPayload(t *testing.T) {
	// Flags + length claiming 100 bytes, but only 2 bytes of payload.
	var buf bytes.Buffer
	buf.WriteByte(0x00)
	_ = binary.Write(&buf, binary.BigEndian, uint32(100))
	buf.Write([]byte("ab"))

	_, err := readConnectFrame(&buf)
	if err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

func TestParseTrailerErrorInvalidJSON(t *testing.T) {
	// Invalid JSON should be silently ignored (returns nil).
	if err := parseTrailerError([]byte("{invalid")); err != nil {
		t.Errorf("expected nil for invalid JSON, got %v", err)
	}
}

func TestConnectEnvelopeEmpty(t *testing.T) {
	envelope, err := connectEnvelope(nil)
	if err != nil {
		t.Fatalf("connectEnvelope: %v", err)
	}
	// 1 byte flags + 4 bytes length (0) + 0 bytes payload.
	if len(envelope) != 5 {
		t.Errorf("length = %d, want 5", len(envelope))
	}
	msgLen := binary.BigEndian.Uint32(envelope[1:5])
	if msgLen != 0 {
		t.Errorf("encoded length = %d, want 0", msgLen)
	}
}

func makeFrame(flags byte, payload []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(flags)
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(payload)))
	buf.Write(payload)
	return buf.Bytes()
}
