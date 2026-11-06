package profile

import (
	"encoding/json"
)

// UnmarshalBlocks decodes a JSON array of blocks — how the store keeps a
// file's blocks — into []Block, with the result json.Unmarshal would give.
//
// It is the read behind every page that shows a file's lines, and a large
// commit carries hundreds of thousands of blocks, where reflection-driven
// decoding was most of the repo page's time. So the shape the store writes
// — an array of objects whose keys are Block's json names and whose
// values are plain integers, in any order and with any spacing (Postgres
// jsonb reorders and spaces them) — is scanned by hand, and anything else
// (an unknown key, an escape, a fraction or exponent, a number too long
// to be exact, null) goes to json.Unmarshal, so the two never disagree.
func UnmarshalBlocks(data []byte) ([]Block, error) {
	if blocks, ok := scanBlocks(data); ok {
		return blocks, nil
	}
	var blocks []Block
	if err := json.Unmarshal(data, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// scanBlocks is UnmarshalBlocks' fast path; ok is false for any input it
// does not fully understand.
func scanBlocks(data []byte) (blocks []Block, ok bool) {
	s := blockScanner{data: data}
	s.space()
	if !s.take('[') {
		return nil, false
	}
	// A stored block is about 90 bytes of JSON.
	blocks = make([]Block, 0, len(data)/90+1)
	s.space()
	if s.take(']') {
		return blocks, s.end()
	}
	for {
		blk, ok := s.block()
		if !ok {
			return nil, false
		}
		blocks = append(blocks, blk)
		s.space()
		if s.take(',') {
			continue
		}
		if s.take(']') {
			return blocks, s.end()
		}
		return nil, false
	}
}

type blockScanner struct {
	data []byte
	i    int
}

func (s *blockScanner) space() {
	for s.i < len(s.data) {
		switch s.data[s.i] {
		case ' ', '\t', '\n', '\r':
			s.i++
		default:
			return
		}
	}
}

func (s *blockScanner) take(c byte) bool {
	if s.i < len(s.data) && s.data[s.i] == c {
		s.i++
		return true
	}
	return false
}

// end reports whether only whitespace is left.
func (s *blockScanner) end() bool {
	s.space()
	return s.i == len(s.data)
}

// block scans one {"key": int, ...} object.
func (s *blockScanner) block() (Block, bool) {
	var b Block
	s.space()
	if !s.take('{') {
		return b, false
	}
	s.space()
	if s.take('}') {
		return b, true
	}
	for {
		s.space()
		key, ok := s.key()
		if !ok {
			return b, false
		}
		s.space()
		if !s.take(':') {
			return b, false
		}
		s.space()
		v, ok := s.int()
		if !ok {
			return b, false
		}
		switch string(key) { // compared without allocating
		case "start_line":
			b.StartLine = v
		case "start_col":
			b.StartCol = v
		case "end_line":
			b.EndLine = v
		case "end_col":
			b.EndCol = v
		case "num_stmts":
			b.NumStmts = v
		case "count":
			b.Count = v
		default:
			// json.Unmarshal matches keys case-insensitively and skips
			// unknown ones; leave both to it.
			return b, false
		}
		s.space()
		if s.take(',') {
			continue
		}
		return b, s.take('}')
	}
}

// key scans a quoted key with no escapes in it.
func (s *blockScanner) key() ([]byte, bool) {
	if !s.take('"') {
		return nil, false
	}
	start := s.i
	for s.i < len(s.data) {
		switch c := s.data[s.i]; {
		case c == '"':
			key := s.data[start:s.i]
			s.i++
			return key, true
		case c == '\\' || c < 0x20:
			return nil, false
		}
		s.i++
	}
	return nil, false
}

// maxIntDigits keeps a scanned integer exact in an int on every platform
// gocov builds for; longer numbers go to json.Unmarshal, which reports
// the overflow.
const maxIntDigits = 9

// int scans a JSON integer: an optional minus, then 0 or a digit string
// without a leading zero. A fraction or exponent after it fails the
// caller's next expectation and falls back.
func (s *blockScanner) int() (int, bool) {
	neg := s.take('-')
	start := s.i
	v := 0
	for s.i < len(s.data) && s.data[s.i] >= '0' && s.data[s.i] <= '9' {
		v = v*10 + int(s.data[s.i]-'0')
		s.i++
	}
	digits := s.i - start
	if digits == 0 || digits > maxIntDigits || (digits > 1 && s.data[start] == '0') {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}
