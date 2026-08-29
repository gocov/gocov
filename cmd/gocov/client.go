package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

type uploadRequest struct {
	Server       string
	Token        string // empty on a tokenless upload
	Format       string
	PathPrefix   string
	Part         string
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
	if req.Token == "" {
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
	filename := req.ProfileName
	if filename == "" {
		filename = "coverage.out"
	}
	fw, err := mw.CreateFormFile("profile", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(req.ProfileData); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	url := strings.TrimSuffix(req.Server, "/") + "/api/v1/upload"
	httpReq, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		return nil, err
	}
	if req.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	}
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != "" {
			return nil, &serverError{code: resp.StatusCode, msg: apiErr.Error}
		}
		return nil, &serverError{code: resp.StatusCode, msg: string(body)}
	}
	var out uploadResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &out, nil
}
