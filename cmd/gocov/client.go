package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

type uploadRequest struct {
	Server string
	Token  string // empty on a tokenless or OIDC upload
	// OIDCToken is a forge-minted OIDC identity token, sent in place of a
	// bearer token when the workflow has the id-token permission but no
	// pasted GOCOV_TOKEN. Empty otherwise.
	OIDCToken    string
	Format       string
	PathPrefix   string
	Part         string
	Ignore       []string // ignore patterns, one `ignore` field each
	ProfileData  []byte
	ProfileName  string
	Uploader     string
	UploaderKind string
	Build        buildInfo
	Meta         metaInfo
	// Run is the workflow-run claim a tokenless upload authenticates
	// with; zero fields when a token is sent.
	Run runInfo
}

// uploadResponse mirrors the server's POST /api/v1/upload response.
type uploadResponse struct {
	ID           int64    `json:"id"`
	TotalPct     float64  `json:"total_pct"`
	CoveredStmts int64    `json:"covered_stmts"`
	TotalStmts   int64    `json:"total_stmts"`
	DeltaPct     *float64 `json:"delta_pct"`
	BuildStatus  string   `json:"build_status"`
	CodeInsights string   `json:"code_insights"`
	RepoCreated  bool     `json:"repo_created"`
	IgnoredFiles int      `json:"ignored_files"`
	Gate         string   `json:"gate"`

	DiffPct          *float64 `json:"diff_pct"`
	DiffCoveredLines *int64   `json:"diff_covered_lines"`
	DiffTotalLines   *int64   `json:"diff_total_lines"`
	DiffStatus       string   `json:"diff_status"`
	PRComment        string   `json:"pr_comment"`
}

// serverError is a non-2xx answer from the server, kept as a type so
// tokenless mode can tell a rejection (server said no, with a reason)
// from transport trouble.
type serverError struct {
	code int
	msg  string
}

func (e *serverError) Error() string {
	return fmt.Sprintf("server returned %d: %s", e.code, e.msg)
}

// httpDoer is the slice of *http.Client the CLI's requests need, kept as
// an interface so tests can stub the OIDC mint.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// maxResponseBytes bounds how much of an answer the CLI reads: an upload
// receipt or an id-token is a few kilobytes, so anything past a megabyte
// is not a response worth holding in memory.
const maxResponseBytes = 1 << 20

// send performs req and returns the status and the bounded body, so
// callers keep only what the answer means to them.
func send(doer httpDoer, req *http.Request) (int, []byte, error) {
	resp, err := doer.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

func upload(req uploadRequest) (*uploadResponse, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fields := map[string]string{
		"repo":           req.Build.Repo,
		"commit":         req.Build.Commit,
		"branch":         req.Build.Branch,
		"pr_id":          req.Build.PRID,
		"format":         req.Format,
		"path_prefix":    req.PathPrefix,
		"part":           req.Part,
		"uploader":       req.Uploader,
		"uploader_kind":  req.UploaderKind,
		"ci_provider":    req.Meta.CIProvider,
		"ci_run_url":     req.Meta.CIRunURL,
		"commit_message": req.Meta.CommitMessage,
		"commit_author":  req.Meta.CommitAuthor,
	}
	switch {
	case req.Token != "":
		// Bearer token authenticates; no form credential needed.
	case req.OIDCToken != "":
		fields["oidc_token"] = req.OIDCToken
	default:
		fields["run_id"] = req.Run.RunID
		fields["run_attempt"] = req.Run.RunAttempt
		fields["head_repo"] = req.Run.HeadRepo
	}
	for k, v := range fields {
		if v == "" {
			continue
		}
		if err := mw.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	for _, p := range req.Ignore {
		if err := mw.WriteField("ignore", p); err != nil {
			return nil, err
		}
	}
	fw, err := mw.CreateFormFile("profile", cmp.Or(req.ProfileName, "coverage.out"))
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(req.ProfileData); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	endpoint := strings.TrimSuffix(req.Server, "/") + "/api/v1/upload"
	httpReq, err := http.NewRequest(http.MethodPost, endpoint, &buf)
	if err != nil {
		return nil, err
	}
	if req.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	}
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())

	status, body, err := send(&http.Client{Timeout: 60 * time.Second}, httpReq)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != "" {
			return nil, &serverError{code: status, msg: apiErr.Error}
		}
		return nil, &serverError{code: status, msg: string(body)}
	}
	var out uploadResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &out, nil
}
