package diffcov

import "sort"

// Merge combines the diff-coverage results of the separate parts of one
// commit into a single result, so a changed line counted as covered by any
// part is covered overall. Every part is evaluated against the same pull
// request diff, so the changed-line set of a given file is the same across
// parts; a line is uncovered in the merge only when every part that has
// data for the file left it uncovered.
//
// Combining the parts' already-computed results (rather than recomputing
// from a merged profile) sidesteps the parts' differing path prefixes:
// each part mapped its own profile paths to repo-relative paths when it
// ran, and FileCoverage.Path is already repo-relative. Nil inputs are
// ignored; Merge returns nil when nothing has diff data.
func Merge(results ...*Result) *Result {
	var present []*Result
	for _, r := range results {
		if r != nil {
			present = append(present, r)
		}
	}
	if len(present) == 0 {
		return nil
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
	for _, path := range order {
		out.Files = append(out.Files, mergeFile(path, byPath[path]))
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
	return out
}

// mergeFile intersects the uncovered lines of one changed file across the
// parts that have data for it. The parts share the file's changed-line set
// (same diff), so a line survives as uncovered only when every part left it
// uncovered; covered follows from the total.
func mergeFile(path string, parts []FileCoverage) FileCoverage {
	// Count how many parts leave each line uncovered; a line is uncovered
	// in the merge only when all of them do.
	uncoveredCount := make(map[int]int)
	for _, f := range parts {
		for _, ln := range f.UncoveredLines {
			uncoveredCount[ln]++
		}
	}
	var total int64
	for _, f := range parts {
		if f.TotalLines > total {
			total = f.TotalLines
		}
	}
	var uncovered []int
	for ln, n := range uncoveredCount {
		if n == len(parts) {
			uncovered = append(uncovered, ln)
		}
	}
	sort.Ints(uncovered)
	return FileCoverage{
		Path:           path,
		TotalLines:     total,
		CoveredLines:   total - int64(len(uncovered)),
		UncoveredLines: uncovered,
	}
}
