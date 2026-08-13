package profile

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	t.Run("disjoint files are unioned", func(t *testing.T) {
		backend := &Profile{Files: []File{
			{Path: "a.go", Blocks: []Block{{StartLine: 1, EndLine: 2, NumStmts: 3, Count: 1}}},
		}}
		frontend := &Profile{Files: []File{
			{Path: "b.ts", Blocks: []Block{{StartLine: 1, EndLine: 2, NumStmts: 2, Count: 0}}},
		}}
		m := Merge(backend, frontend)
		c, total := m.Coverage()
		if c != 3 || total != 5 {
			t.Errorf("coverage = %d/%d, want 3/5", c, total)
		}
		if len(m.Files) != 2 || m.Files[0].Path != "a.go" || m.Files[1].Path != "b.ts" {
			t.Errorf("files = %+v, want a.go then b.ts (sorted)", m.Files)
		}
	})

	t.Run("same file same blocks sums counts", func(t *testing.T) {
		// Two matrix shards of one suite cover the same file; the shared
		// block must not be counted twice.
		shard := func(count int) *Profile {
			return &Profile{Files: []File{
				{Path: "a.go", Blocks: []Block{{StartLine: 1, EndLine: 2, NumStmts: 4, Count: count}}},
			}}
		}
		m := Merge(shard(0), shard(2))
		c, total := m.Coverage()
		if total != 4 {
			t.Errorf("total = %d, want 4 (block counted once, not doubled)", total)
		}
		if c != 4 {
			t.Errorf("covered = %d, want 4 (covered by the second shard)", c)
		}
		if len(m.Files[0].Blocks) != 1 || m.Files[0].Blocks[0].Count != 2 {
			t.Errorf("block = %+v, want one block with count 2", m.Files[0].Blocks)
		}
	})

	t.Run("set-mode merges as OR", func(t *testing.T) {
		// Go set mode records 0/1; a line covered in either part is covered.
		a := &Profile{Files: []File{{Path: "a.go", Blocks: []Block{
			{StartLine: 1, EndLine: 2, NumStmts: 1, Count: 1},
			{StartLine: 3, EndLine: 4, NumStmts: 1, Count: 0},
		}}}}
		b := &Profile{Files: []File{{Path: "a.go", Blocks: []Block{
			{StartLine: 1, EndLine: 2, NumStmts: 1, Count: 0},
			{StartLine: 3, EndLine: 4, NumStmts: 1, Count: 1},
		}}}}
		m := Merge(a, b)
		c, total := m.Coverage()
		if c != 2 || total != 2 {
			t.Errorf("coverage = %d/%d, want 2/2 (both blocks covered by some part)", c, total)
		}
	})

	t.Run("nil inputs ignored", func(t *testing.T) {
		p := &Profile{Files: []File{{Path: "a.go", Blocks: []Block{{NumStmts: 1, Count: 1}}}}}
		m := Merge(nil, p, nil)
		if !reflect.DeepEqual(m.Files, p.Files) {
			t.Errorf("merge with nils = %+v, want %+v", m.Files, p.Files)
		}
	})

	t.Run("empty merge is empty", func(t *testing.T) {
		m := Merge()
		if len(m.Files) != 0 {
			t.Errorf("empty merge = %+v, want no files", m.Files)
		}
	})
}
