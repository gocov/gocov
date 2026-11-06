package profile

import (
	"encoding/xml"
	"fmt"
	"io"
)

// OpenCoverParser parses OpenCover XML reports — written by OpenCover
// itself and by Coverlet's opencover output (coverage.opencover.xml), the
// usual coverage formats of .NET. Line hits come from each method's
// <SequencePoint> elements: a point spanning several lines (sl..el) counts
// its visits on every one of them, and points sharing a line sum, as
// Coverlet does when it writes the same data as Cobertura. Branch points
// are ignored, matching the other line-oriented parsers.
type OpenCoverParser struct{}

type openCoverSession struct {
	XMLName xml.Name          `xml:"CoverageSession"`
	Modules []openCoverModule `xml:"Modules>Module"`
}

type openCoverModule struct {
	Files   []openCoverFile  `xml:"Files>File"`
	Classes []openCoverClass `xml:"Classes>Class"`
}

type openCoverFile struct {
	UID      string `xml:"uid,attr"`
	FullPath string `xml:"fullPath,attr"`
}

type openCoverClass struct {
	Methods []openCoverMethod `xml:"Methods>Method"`
}

type openCoverMethod struct {
	FileRef struct {
		UID string `xml:"uid,attr"`
	} `xml:"FileRef"`
	SequencePoints []openCoverPoint `xml:"SequencePoints>SequencePoint"`
}

type openCoverPoint struct {
	VC     int    `xml:"vc,attr"`
	SL     int    `xml:"sl,attr"`
	EL     int    `xml:"el,attr"`
	FileID string `xml:"fileid,attr"`
}

// openCoverHiddenLine is the line number the .NET compilers give hidden
// sequence points (compiler-generated code with no source line); OpenCover
// copies it through, and it is no line of any file.
const openCoverHiddenLine = 0xFEEFEE

// maxOpenCoverLineMarks bounds the per-line work a report may ask for. A
// sequence point costs one map update per line it spans, so a small
// hostile report of many points each spanning millions of lines would
// otherwise cost far more than its size. Real reports mark each executable
// line about once, so this leaves room for codebases of millions of lines.
const maxOpenCoverLineMarks = 4_000_000

// Parse implements Parser. File uids resolve within their module first
// and then across the session, since OpenCover numbers files per session
// but lists them per module; a point without a fileid falls back to its
// method's <FileRef>. Repeated entries for one path merge by summing hits.
func (OpenCoverParser) Parse(r io.Reader) (*Profile, error) {
	var report openCoverSession
	if err := xml.NewDecoder(r).Decode(&report); err != nil {
		return nil, fmt.Errorf("opencover: %w", err)
	}

	session := map[string]string{}                           // uid -> path, across modules
	locals := make([]map[string]string, len(report.Modules)) // uid -> path, per module
	for i, m := range report.Modules {
		locals[i] = make(map[string]string, len(m.Files))
		for _, f := range m.Files {
			if f.UID != "" && f.FullPath != "" {
				path := normalizePath(f.FullPath)
				locals[i][f.UID] = path
				session[f.UID] = path
			}
		}
	}

	files := map[string]map[int]int{} // path -> line -> summed hits
	marks := 0
	for i, m := range report.Modules {
		local := locals[i]
		for _, cls := range m.Classes {
			for _, meth := range cls.Methods {
				for _, sp := range meth.SequencePoints {
					if sp.SL == openCoverHiddenLine {
						continue
					}
					uid := sp.FileID
					if uid == "" {
						uid = meth.FileRef.UID
					}
					path, ok := local[uid]
					if !ok {
						path, ok = session[uid]
					}
					if !ok {
						return nil, fmt.Errorf("opencover: sequence point refers to unknown file %q", uid)
					}
					end := max(sp.EL, sp.SL) // some writers leave el unset on one-line points
					if sp.SL <= 0 || end > maxLineNumber {
						return nil, fmt.Errorf("opencover: malformed line range %d-%d in %s", sp.SL, sp.EL, path)
					}
					if sp.VC < 0 {
						return nil, fmt.Errorf("opencover: negative visit count in %s line %d", path, sp.SL)
					}
					marks += end - sp.SL + 1
					if marks > maxOpenCoverLineMarks {
						return nil, fmt.Errorf("opencover: sequence points span more than %d lines in total", maxOpenCoverLineMarks)
					}
					lines := files[path]
					if lines == nil {
						lines = map[int]int{}
						files[path] = lines
					}
					for ln := sp.SL; ln <= end; ln++ {
						lines[ln] += sp.VC
					}
				}
			}
		}
	}

	return fromLineHits("opencover", files)
}
