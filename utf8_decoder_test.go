package e2b

import (
	"strings"
	"testing"
)

func TestUTF8DecoderASCII(t *testing.T) {
	var d incrementalUTF8Decoder
	if got := d.Decode([]byte("hello")); got != "hello" {
		t.Errorf("Decode = %q, want %q", got, "hello")
	}
	if got := d.Flush(); got != "" {
		t.Errorf("Flush = %q, want empty", got)
	}
}

func TestUTF8DecoderEmptyChunk(t *testing.T) {
	var d incrementalUTF8Decoder
	if got := d.Decode(nil); got != "" {
		t.Errorf("Decode(nil) = %q, want empty", got)
	}
	if got := d.Decode([]byte{}); got != "" {
		t.Errorf("Decode(empty) = %q, want empty", got)
	}
}

func TestUTF8DecoderMultibyteWhole(t *testing.T) {
	var d incrementalUTF8Decoder
	// "héllo wörld 🌍" contains 2-byte, and 4-byte runes.
	input := "héllo wörld 🌍"
	if got := d.Decode([]byte(input)); got != input {
		t.Errorf("Decode = %q, want %q", got, input)
	}
	if got := d.Flush(); got != "" {
		t.Errorf("Flush = %q, want empty", got)
	}
}

func TestUTF8DecoderSplitTwoByteRune(t *testing.T) {
	var d incrementalUTF8Decoder
	// "é" is 0xC3 0xA9. Split it across two chunks.
	full := []byte("é")
	if len(full) != 2 {
		t.Fatalf("expected 2-byte encoding, got %d", len(full))
	}
	if got := d.Decode(full[:1]); got != "" {
		t.Errorf("first half Decode = %q, want empty (buffered)", got)
	}
	if got := d.Decode(full[1:]); got != "é" {
		t.Errorf("second half Decode = %q, want %q", got, "é")
	}
	if got := d.Flush(); got != "" {
		t.Errorf("Flush = %q, want empty", got)
	}
}

func TestUTF8DecoderSplitFourByteRune(t *testing.T) {
	var d incrementalUTF8Decoder
	// "🌍" is 4 bytes (0xF0 0x9F 0x8C 0x8D). Feed one byte at a time.
	full := []byte("🌍")
	if len(full) != 4 {
		t.Fatalf("expected 4-byte encoding, got %d", len(full))
	}
	var out strings.Builder
	for i := 0; i < len(full); i++ {
		out.WriteString(d.Decode(full[i : i+1]))
	}
	out.WriteString(d.Flush())
	if out.String() != "🌍" {
		t.Errorf("reassembled = %q, want %q", out.String(), "🌍")
	}
}

func TestUTF8DecoderSplitAcrossTextBoundary(t *testing.T) {
	var d incrementalUTF8Decoder
	// A rune split where the first chunk ends mid-rune but has leading text.
	full := []byte("ab🌍cd")
	// Split so the emoji is broken: "ab" + first 2 emoji bytes, then rest.
	first := full[:4]  // "ab" + 2 bytes of emoji
	second := full[4:] // remaining 2 emoji bytes + "cd"

	var out strings.Builder
	out.WriteString(d.Decode(first))  // should emit "ab", buffer partial emoji
	out.WriteString(d.Decode(second)) // should emit "🌍cd"
	out.WriteString(d.Flush())
	if out.String() != "ab🌍cd" {
		t.Errorf("reassembled = %q, want %q", out.String(), "ab🌍cd")
	}
}

func TestUTF8DecoderInvalidByteFlushed(t *testing.T) {
	var d incrementalUTF8Decoder
	// 0xFF is never valid in UTF-8. It's a full (invalid) rune, so it should
	// be replaced immediately, not buffered.
	got := d.Decode([]byte{0xFF})
	if got != "�" {
		t.Errorf("Decode(0xFF) = %q, want replacement char", got)
	}
	if got := d.Flush(); got != "" {
		t.Errorf("Flush = %q, want empty", got)
	}
}

func TestUTF8DecoderIncompleteTrailingFlushed(t *testing.T) {
	var d incrementalUTF8Decoder
	// Leading byte of a 3-byte rune with nothing after it: incomplete, so it
	// is buffered by Decode and only surfaced (as replacement) by Flush.
	if got := d.Decode([]byte{0xE2}); got != "" {
		t.Errorf("Decode = %q, want empty (buffered)", got)
	}
	if got := d.Flush(); got != "�" {
		t.Errorf("Flush = %q, want replacement char", got)
	}
	// Decoder resets after Flush.
	if got := d.Flush(); got != "" {
		t.Errorf("second Flush = %q, want empty", got)
	}
}

func TestUTF8DecoderInvalidThenValid(t *testing.T) {
	var d incrementalUTF8Decoder
	// Invalid byte followed by valid ASCII in the same chunk.
	got := d.Decode([]byte{0xFF, 'x'})
	if got != "�x" {
		t.Errorf("Decode = %q, want %q", got, "�x")
	}
}
