package profile

import (
	"bytes"
	"encoding/json"
	"strconv"
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
	if !s.take('[') {
		return nil, false
	}
	// A stored block is about 90 bytes of JSON.
	blocks = make([]Block, 0, len(data)/90+1)
	if !s.take(']') {
		for {
			blk, ok := s.block()
			if !ok {
				return nil, false
			}
			blocks = append(blocks, blk)
			if s.take(']') {
				break
			}
			if !s.take(',') {
				return nil, false
			}
		}
	}
	s.space()
	return blocks, s.i == len(s.data)
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

// take skips whitespace, then consumes c if it comes next.
func (s *blockScanner) take(c byte) bool {
	s.space()
	if s.i < len(s.data) && s.data[s.i] == c {
		s.i++
		return true
	}
	return false
}

// jsonbKeys is how Postgres prints a stored block's keys: ordered by
// length, then bytes, with a space after every colon and comma.
var jsonbKeys = [...][]byte{
	[]byte(`{"count": `), []byte(`, "end_col": `), []byte(`, "end_line": `),
	[]byte(`, "num_stmts": `), []byte(`, "start_col": `), []byte(`, "start_line": `),
}

// jsonbBlock scans a block printed exactly the way Postgres prints one,
// matching its keys as one run of literals instead of key by key; on any
// other layout it scans nothing and reports false.
func (s *blockScanner) jsonbBlock() (Block, bool) {
	s.space()
	start := s.i
	var v [len(jsonbKeys)]int
	for k, lit := range jsonbKeys {
		if !bytes.HasPrefix(s.data[s.i:], lit) {
			s.i = start
			return Block{}, false
		}
		s.i += len(lit)
		n, ok := s.digits()
		if !ok {
			s.i = start
			return Block{}, false
		}
		v[k] = n
	}
	if s.i >= len(s.data) || s.data[s.i] != '}' {
		s.i = start
		return Block{}, false
	}
	s.i++
	return Block{Count: v[0], EndCol: v[1], EndLine: v[2], NumStmts: v[3], StartCol: v[4], StartLine: v[5]}, true
}

// block scans one {"key": int, ...} object.
func (s *blockScanner) block() (Block, bool) {
	if b, ok := s.jsonbBlock(); ok {
		return b, true
	}
	var b Block
	if !s.take('{') {
		return b, false
	}
	if s.take('}') {
		return b, true
	}
	for {
		key, ok := s.key()
		if !ok || !s.take(':') {
			return b, false
		}
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
		if !s.take(',') {
			return b, s.take('}')
		}
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

// maxIntDigits keeps a scanned integer exact in an int: 18 digits on a
// 64-bit platform, where a Go profile's hot loop can run past a billion,
// 9 on a 32-bit one. Longer numbers go to json.Unmarshal, which reports
// any overflow.
const maxIntDigits = 9 * strconv.IntSize / 32

// int scans a JSON integer after optional whitespace: an optional minus,
// then digits. A fraction or exponent after it fails the caller's next
// expectation and falls back.
func (s *blockScanner) int() (int, bool) {
	neg := s.take('-')
	v, ok := s.digits()
	if neg {
		v = -v
	}
	return v, ok
}

// digits scans 0 or a digit string without a leading zero, right here.
func (s *blockScanner) digits() (int, bool) {
	start := s.i
	v := 0
	for s.i < len(s.data) && s.data[s.i] >= '0' && s.data[s.i] <= '9' {
		v = v*10 + int(s.data[s.i]-'0')
		s.i++
	}
	n := s.i - start
	if n == 0 || n > maxIntDigits || (n > 1 && s.data[start] == '0') {
		return 0, false
	}
	return v, true
}
