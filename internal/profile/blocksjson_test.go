package profile

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// jsonbBlocks renders n blocks the way Postgres jsonb prints them: keys
// reordered by length, a space after every colon and comma.
func jsonbBlocks(n int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, `{"count": %d, "end_col": 9, "end_line": %d, "num_stmts": %d, "start_col": 2, "start_line": %d}`, i%3, i*4+2, i%5, i*4+1)
	}
	b.WriteString("]")
	return b.String()
}

// The fast path and json.Unmarshal must agree on every input: same blocks,
// or both an error. These are the shapes each side of the split meets.
func TestUnmarshalBlocksAgreesWithEncodingJSON(t *testing.T) {
	marshalled, err := json.Marshal([]Block{{1, 2, 3, 4, 5, 6}, {StartLine: 7, Count: -1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{
		// what the store writes and reads back
		string(marshalled),
		jsonbBlocks(1),
		jsonbBlocks(40),
		"[]",
		" [ ] ",
		`[{}]`,
		"[\n\t{\"count\":1}\r\n]",
		`[{"count": 1, "count": 2}]`, // the last of a repeated key wins
		`[{"count": -0}]`,
		`[{"count": 999999999}]`,
		`[{"count": 1234567890}]`, // a hot loop's count, past 32 bits' 9 digits
		`[{"count": 999999999999999999}]`,
		`[{"count": -999999999999999999}]`,
		// left to json.Unmarshal
		`null`,
		`[null]`,
		`[{"Count": 1}]`,                      // keys match case-insensitively
		`[{"other": 1}]`,                      // unknown keys are skipped
		`[{"count": 1.0}]`,                    // not an integer
		`[{"count": 1e2}]`,                    // not an integer
		`[{"count": 01}]`,                     // not JSON
		`[{"count": 1234567890123456789012}]`, // overflows
		`[{"co\u0075nt": 1}]`,                 // an escaped key
		`[{"count": "1"}]`,
		`[{"count": 1},]`,
		`[{"count": 1}] x`,
		`[{"count": 1}`,
		`[{"count" 1}]`,
		`{}`,
		``,
	} {
		t.Run(in, func(t *testing.T) { compareWithEncodingJSON(t, []byte(in)) })
	}
}

func compareWithEncodingJSON(t *testing.T, in []byte) {
	t.Helper()
	got, gotErr := UnmarshalBlocks(in)
	var want []Block
	wantErr := json.Unmarshal(in, &want)
	if (gotErr != nil) != (wantErr != nil) {
		t.Fatalf("%q: err = %v, json.Unmarshal err = %v", in, gotErr, wantErr)
	}
	if wantErr == nil && !reflect.DeepEqual(got, want) {
		t.Fatalf("%q: blocks = %+v, json.Unmarshal = %+v", in, got, want)
	}
}

func FuzzUnmarshalBlocks(f *testing.F) {
	f.Add(jsonbBlocks(3))
	f.Add(`[{"count":1,"start_line":2}]`)
	f.Add(`[{"count": -12, "end_col": 0}, {}]`)
	f.Fuzz(func(t *testing.T, in string) { compareWithEncodingJSON(t, []byte(in)) })
}

func BenchmarkUnmarshalBlocks(b *testing.B) {
	data := []byte(jsonbBlocks(50))
	b.Run("fast", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			if _, err := UnmarshalBlocks(data); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("encoding/json", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var blocks []Block
			if err := json.Unmarshal(data, &blocks); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// What the store writes stays on the fast path, a hot loop's count past
// a billion included; json.Unmarshal is for the rest.
func TestStoredBlocksTakeTheFastPath(t *testing.T) {
	for _, in := range []string{
		jsonbBlocks(3),
		`[{"count": 1234567890, "end_col": 9, "end_line": 2, "num_stmts": 1, "start_col": 2, "start_line": 1}]`,
		`[{"start_line":1,"start_col":2,"end_line":2,"end_col":9,"num_stmts":1,"count":3}]`, // json.Marshal's order
	} {
		if _, ok := scanBlocks([]byte(in)); !ok {
			t.Errorf("%s fell back to json.Unmarshal", in)
		}
	}
}
