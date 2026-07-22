package e2b

import "unicode/utf8"

// incrementalUTF8Decoder decodes a byte stream into UTF-8 text one chunk at a
// time, correctly handling multi-byte runes that are split across chunk
// boundaries.
//
// The envd process stream delivers stdout/stderr as arbitrary byte frames; a
// single UTF-8 rune can straddle two frames. This decoder buffers a trailing
// partial rune and prepends it to the next chunk, matching the incremental
// decoding behavior of the JS and Python SDKs. Invalid byte sequences are
// emitted as the Unicode replacement character (U+FFFD) once they are known to
// be invalid rather than merely incomplete.
type incrementalUTF8Decoder struct {
	// buf holds bytes carried over from a previous Decode call that did not
	// yet form a complete rune.
	buf []byte
}

// Decode consumes chunk and returns the text that can be fully decoded so far.
// Any incomplete trailing multi-byte sequence is retained internally and
// combined with the next chunk (or surfaced by Flush).
func (d *incrementalUTF8Decoder) Decode(chunk []byte) string {
	if len(chunk) == 0 {
		return ""
	}

	data := chunk
	if len(d.buf) > 0 {
		data = make([]byte, 0, len(d.buf)+len(chunk))
		data = append(data, d.buf...)
		data = append(data, chunk...)
		d.buf = d.buf[:0]
	}

	out := make([]byte, 0, len(data))
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size <= 1 {
			// Either an invalid byte or an incomplete trailing sequence. If the
			// remaining bytes could still become a valid rune once more data
			// arrives, buffer them and stop; otherwise emit a replacement
			// character and advance one byte.
			if !utf8.FullRune(data) {
				d.buf = append(d.buf[:0], data...)
				break
			}
			out = utf8.AppendRune(out, utf8.RuneError)
			data = data[1:]
			continue
		}
		out = append(out, data[:size]...)
		data = data[size:]
	}
	return string(out)
}

// Flush returns any bytes still buffered from an incomplete trailing sequence,
// decoding them with replacement characters. Call it once the stream has ended
// so partial runes are not silently dropped. After Flush the decoder is reset.
func (d *incrementalUTF8Decoder) Flush() string {
	if len(d.buf) == 0 {
		return ""
	}
	out := make([]byte, 0, len(d.buf))
	data := d.buf
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size <= 1 {
			out = utf8.AppendRune(out, utf8.RuneError)
			data = data[1:]
			continue
		}
		out = append(out, data[:size]...)
		data = data[size:]
	}
	d.buf = d.buf[:0]
	return string(out)
}
