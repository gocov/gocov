// The accept log: one line per upload saying where its time went, so a
// slow upload can be told apart into the forge (diff, push), the database
// (gate baseline, blob, insert, recompute) and the request itself, and
// the push apart into its lease claim and each forge surface.

package core

import (
	"time"
)

// acceptTimings is how long each stage of one Accept took. The diff fetch
// runs beside the gate baseline read and the blob write, so the stages do
// not add up to the total.
type acceptTimings struct {
	started  time.Time // when the transport began receiving; zero if unknown
	accepted time.Time // when Accept began

	// stage is the step running now — on failure, the one that failed.
	stage string

	ignore, diff, gateBase, blob, insert, recompute, push time.Duration
}

func newAcceptTimings(started time.Time) *acceptTimings {
	return &acceptTimings{started: started, accepted: time.Now()}
}

// begin starts timing a stage into d and returns the call that stops it.
func (t *acceptTimings) begin(d *time.Duration) func() { return timeInto(d) }

// logAccept writes the accept log line: at info when the upload landed,
// at warn with the failing stage when it did not.
func (p *Pipeline) logAccept(sub Submission, t *acceptTimings, res *Result, err error) {
	if p.Log == nil {
		return
	}
	end := time.Now()
	attrs := []any{
		"repo", sub.Repo.Slug,
		"commit", sub.Commit,
		"part", sub.Part,
		"files", len(sub.Profile.Files),
		"bytes", len(sub.Raw),
	}
	if !t.started.IsZero() {
		attrs = append(attrs, "read_parse_ms", t.accepted.Sub(t.started).Milliseconds())
	}
	attrs = append(attrs,
		"ignore_ms", t.ignore.Milliseconds(),
		"gate_base_ms", t.gateBase.Milliseconds(),
		"blob_ms", t.blob.Milliseconds(),
	)
	if sub.PRID != "" {
		attrs = append(attrs, "diff_ms", t.diff.Milliseconds())
	}
	attrs = append(attrs,
		"insert_ms", t.insert.Milliseconds(),
		"recompute_ms", t.recompute.Milliseconds(),
		"push_ms", t.push.Milliseconds(),
	)
	// push_ms split: claiming the commit's push lease, then each surface's
	// forge round trip (side by side, so the push takes as long as the
	// slowest). Left out without a forge connection; the surfaces are left
	// out when none reached the forge (superseded, queued).
	if pt := res.pushTimings(); pt.tried {
		attrs = append(attrs, "push_claim_ms", pt.claim.Milliseconds())
		if pt.ran {
			attrs = append(attrs,
				"status_ms", pt.status.Milliseconds(),
				"insights_ms", pt.insights.Milliseconds(),
			)
			if sub.PRID != "" {
				attrs = append(attrs, "comment_ms", pt.comment.Milliseconds())
			}
		}
	}
	attrs = append(attrs, "accept_ms", end.Sub(t.accepted).Milliseconds())
	if !t.started.IsZero() {
		attrs = append(attrs, "total_ms", end.Sub(t.started).Milliseconds())
	}
	if err != nil {
		p.Log.Warn("upload failed", append(attrs, "stage", t.stage, "err", err)...)
		return
	}
	p.Log.Info("upload accepted", append(attrs,
		"parts", len(res.Merged.Parts),
		"build_status", res.Push.BuildStatus,
	)...)
}

// pushTimings is the push's split, zero when the upload did not land.
func (r *Result) pushTimings() pushTimings {
	if r == nil {
		return pushTimings{}
	}
	return r.Push.timings
}
