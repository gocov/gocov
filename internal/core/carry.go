// Carrying parts forward. A CI that runs some of its coverage jobs only
// when their paths change uploads only those parts for a commit, and its
// merged report would drop the rest. The parts a repo names
// (Repo.CarryforwardParts) are instead taken, for a commit that did not
// upload them, from the history before it: the upload that last measured
// each one.
//
// gocov knows no commit's parents, so the history is its own — the
// commit's branch, then the default branch, among the reports seen before
// the commit was (the same order CommitBaseline keeps), so a CI re-run on
// an old commit never takes a part from one that came after it. A carried
// part feeds the totals, the gate and every page the merged report drives,
// but not the diff coverage: its diff was measured against another commit.

package core

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/gocov/gocov/internal/store"
)

// CarriedPart is a part a commit's merged report took from an earlier
// commit: its name, and the upload that measured it.
type CarriedPart struct {
	Part      string
	CommitSHA string
	UploadID  int64
}

// CarriedParts names the uploads among a commit's report uploads
// (store.ReportUploads) that it carries forward — those of other commits —
// ordered by part.
func CarriedParts(commitSHA string, uploads []*store.Upload) []CarriedPart {
	var out []CarriedPart
	for _, u := range uploads {
		if u.CommitSHA != commitSHA {
			out = append(out, CarriedPart{Part: u.Part, CommitSHA: u.CommitSHA, UploadID: u.ID})
		}
	}
	slices.SortFunc(out, func(a, b CarriedPart) int { return cmp.Compare(a.Part, b.Part) })
	return out
}

// carryReads is what carryForward reads; a commit-report transaction
// answers it while the merge runs.
type carryReads interface {
	CarrySources(ctx context.Context, repoID int64, branch, commitSHA string, parts []string, depth int) ([]*store.Upload, error)
}

// carryForward returns the uploads commit's report carries forward: for
// each of the repo's carried parts the commit did not upload itself (own),
// the upload of it in the newest of the last baselineSearchDepth reports
// on branch that has one — its own or one it carried in turn, so a chain
// always ends at the upload that measured the part — then, for the parts
// still missing, on the default branch. One read per branch, and none at
// all when the commit uploaded every carried part. A part neither history
// has is left out, as it would be without carryforward.
func carryForward(ctx context.Context, tx carryReads, repo *store.Repo, commit, branch string, own []*store.Upload) ([]*store.Upload, error) {
	missing := slices.DeleteFunc(slices.Clone(repo.CarryforwardParts), func(name string) bool { return hasPart(own, name) })
	branches := []string{branch}
	if branch != repo.DefaultBranch {
		branches = append(branches, repo.DefaultBranch)
	}
	var carried []*store.Upload
	for _, b := range branches {
		if len(missing) == 0 {
			break
		}
		found, err := tx.CarrySources(ctx, repo.ID, b, commit, missing, baselineSearchDepth)
		if err != nil {
			return nil, fmt.Errorf("loading parts to carry forward: %w", err)
		}
		carried = append(carried, found...)
		missing = slices.DeleteFunc(missing, func(name string) bool { return hasPart(found, name) })
	}
	return carried, nil
}

// hasPart reports whether one of ups is an upload of part name.
func hasPart(ups []*store.Upload, name string) bool {
	return slices.ContainsFunc(ups, func(u *store.Upload) bool { return u.Part == name })
}
