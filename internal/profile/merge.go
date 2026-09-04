package profile

import (
	"cmp"
	"slices"
)

// Merge combines coverage profiles into one, summing the hit counts of
// blocks that share a position so a statement covered by any input is
// covered in the result. It is the cross-report equivalent of the
// per-report block merge the format parsers already perform (see the "go"
// and lcov parsers), lifted one level up to merge the separate parts of a
// single commit — backend, frontend, e2e — into one report.
//
// Set-mode profiles carry counts of 0 or 1; summing them still leaves a
// block covered exactly when some part covered it, so set-mode merges as a
// logical OR without special-casing. Nil inputs are ignored. Files are
// keyed by their profile path and blocks by their position, so identical
// files uploaded by two parts (a matrix rerun of one suite) merge instead
// of double-counting.
func Merge(profiles ...*Profile) *Profile {
	type key struct{ startLine, startCol, endLine, endCol int }
	byFile := make(map[string]map[key]Block)
	for _, p := range profiles {
		if p == nil {
			continue
		}
		for i := range p.Files {
			f := &p.Files[i]
			blocks := byFile[f.Path]
			if blocks == nil {
				blocks = make(map[key]Block)
				byFile[f.Path] = blocks
			}
			for _, b := range f.Blocks {
				k := key{b.StartLine, b.StartCol, b.EndLine, b.EndCol}
				if ex, ok := blocks[k]; ok {
					ex.Count += b.Count
					// Identical positions describe the same statements, so
					// NumStmts should already match; keep the larger to stay
					// safe against a malformed input rather than trust it.
					if b.NumStmts > ex.NumStmts {
						ex.NumStmts = b.NumStmts
					}
					blocks[k] = ex
				} else {
					blocks[k] = b
				}
			}
		}
	}

	out := &Profile{}
	paths := make([]string, 0, len(byFile))
	for path := range byFile {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		blocks := byFile[path]
		merged := make([]Block, 0, len(blocks))
		for _, b := range blocks {
			merged = append(merged, b)
		}
		slices.SortFunc(merged, func(a, b Block) int {
			if c := cmp.Compare(a.StartLine, b.StartLine); c != 0 {
				return c
			}
			return cmp.Compare(a.StartCol, b.StartCol)
		})
		out.Files = append(out.Files, File{Path: path, Blocks: merged})
	}
	return out
}
