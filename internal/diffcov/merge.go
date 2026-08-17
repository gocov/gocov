package diffcov

import "sort"

// Merge combines the diff-coverage results of the separate parts of one
// commit into a single result, so a changed line counted as covered by any
// part is covered overall. Combining the parts' already-computed results
// (rather than recomputing from a merged profile) sidesteps the parts'
// differing path prefixes and per-format path matching: each part already
// mapped its own profile paths to the repo-relative FileCoverage.Path.
//
// The second return value lists repo-relative paths whose parts disagreed
// on the file's changed-line count and so were merged conservatively (see
// mergeFile); the caller can surface it. Nil inputs are ignored; Merge
// returns (nil, nil) when nothing has diff data.
func Merge(results ...*Result) (*Result, []string) {
	var present []*Result
	for _, r := range results {
		if r != nil {
			present = append(present, r)
		}
	}
	if len(present) == 0 {
		return nil, nil
	}

	// Group each changed file's per-part coverage by its repo-relative path.
	byPath := make(map[string][]FileCoverage)
	var order []string
	for _, r := range present {
		for _, f := range r.Files {
			if _, seen := byPath[f.Path]; !seen {
				order = append(order, f.Path)
			}
			byPath[f.Path] = append(byPath[f.Path], f)
		}
	}

	out := &Result{}
	var conflicts []string
	for _, path := range order {
		fc, conflicted := mergeFile(path, byPath[path])
		out.Files = append(out.Files, fc)
		if conflicted {
			conflicts = append(conflicts, path)
		}
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	for i := range out.Files {
		out.CoveredLines += out.Files[i].CoveredLines
		out.TotalLines += out.Files[i].TotalLines
	}

	// A changed file is unmatched only when no part had coverage data for
	// it: union the parts' unmatched lists, then drop any path some part
	// actually covered.
	seenUnmatched := make(map[string]bool)
	for _, r := range present {
		for _, p := range r.UnmatchedFiles {
			if !seenUnmatched[p] && byPath[p] == nil {
				seenUnmatched[p] = true
				out.UnmatchedFiles = append(out.UnmatchedFiles, p)
			}
		}
	}
	sort.Strings(out.UnmatchedFiles)
	sort.Strings(conflicts)
	return out, conflicts
}

// mergeFile merges one changed file's coverage across the parts that have
// data for it, returning the merged coverage and whether the parts had to
// be reconciled conservatively.
//
// When the parts agree on the file's changed-line count they are measuring
// the same set of executable changed lines (the same tool over the same
// diff — e.g. matrix shards of one suite). A line is then covered when any
// part covered it, i.e. uncovered only when every part left it uncovered.
//
// When the parts disagree on the count, their changed-line sets differ
// (typically two different coverage tools reporting the same file), and a
// line absent from a part's uncovered list cannot be assumed covered by
// that part — it may simply not be in that part's set. The counts alone
// can't align covered lines across differing sets, so we fall back to the
// pessimistic union: a line is uncovered if any part left it uncovered.
// That never overstates coverage, and the conflict is reported so it is not
// silent. (Attributing coverage exactly across differing sets needs the
// covered-line numbers, not just counts — tracked as a follow-up.)
func mergeFile(path string, parts []FileCoverage) (FileCoverage, bool) {
	total := parts[0].TotalLines
	agree := true
	for _, f := range parts[1:] {
		if f.TotalLines != total {
			agree = false
		}
		if f.TotalLines > total {
			total = f.TotalLines
		}
	}

	uncoveredCount := make(map[int]int)
	for _, f := range parts {
		for _, ln := range f.UncoveredLines {
			uncoveredCount[ln]++
		}
	}
	var uncovered []int
	for ln, n := range uncoveredCount {
		// Agree: uncovered only when all parts left it uncovered (optimistic).
		// Disagree: uncovered when any part did (pessimistic union).
		if (agree && n == len(parts)) || !agree {
			uncovered = append(uncovered, ln)
		}
	}
	sort.Ints(uncovered)
	// When parts disagree, the union of their uncovered lines can exceed the
	// largest part's total (each part only saw its own changed lines), which
	// would make CoveredLines negative. The distinct uncovered lines are all
	// changed lines, so the total is at least that many.
	if int64(len(uncovered)) > total {
		total = int64(len(uncovered))
	}
	return FileCoverage{
		Path:           path,
		TotalLines:     total,
		CoveredLines:   total - int64(len(uncovered)),
		UncoveredLines: uncovered,
	}, !agree
}
