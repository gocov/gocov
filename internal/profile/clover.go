package profile

import (
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// CloverParser parses Clover XML reports — PHPUnit's --coverage-clover
// output and the "clover" reporter of Istanbul/Jest. Only stmt and cond
// lines contribute: method lines mark declarations rather than executable
// statements, matching how the LCOV parser ignores FN records.
type CloverParser struct{}

type cloverCoverage struct {
	XMLName xml.Name      `xml:"coverage"`
	Project cloverProject `xml:"project"`
}

type cloverProject struct {
	Files    []cloverFile    `xml:"file"`
	Packages []cloverPackage `xml:"package"`
}

type cloverPackage struct {
	Files []cloverFile `xml:"file"`
}

type cloverFile struct {
	Name  string       `xml:"name,attr"`
	Path  string       `xml:"path,attr"`
	Lines []cloverLine `xml:"line"`
}

type cloverLine struct {
	Num   int    `xml:"num,attr"`
	Type  string `xml:"type,attr"`
	Count int    `xml:"count,attr"`
}

// Parse implements Parser. Files may sit directly under <project> or inside
// <package> elements; repeated entries for the same path merge by summing
// per-line hits.
func (CloverParser) Parse(r io.Reader) (*Profile, error) {
	var report cloverCoverage
	if err := xml.NewDecoder(r).Decode(&report); err != nil {
		return nil, fmt.Errorf("clover: %w", err)
	}

	all := report.Project.Files
	for _, pkg := range report.Project.Packages {
		all = append(all, pkg.Files...)
	}

	files := map[string]map[int]int{} // path -> line -> summed hits
	for _, f := range all {
		// Istanbul puts the full path in a path attribute and only the
		// basename in name; PHPUnit uses name alone.
		name := f.Path
		if name == "" {
			name = f.Name
		}
		if name == "" {
			return nil, errors.New("clover: file without a name")
		}
		path := strings.TrimPrefix(strings.ReplaceAll(name, `\`, "/"), "./")
		lines := files[path]
		if lines == nil {
			lines = map[int]int{}
			files[path] = lines
		}
		for _, l := range f.Lines {
			if l.Type == "method" {
				continue
			}
			if l.Num <= 0 || l.Num > maxLineNumber {
				return nil, fmt.Errorf("clover: malformed line number %d in %s", l.Num, path)
			}
			if l.Count < 0 {
				return nil, fmt.Errorf("clover: negative hit count in %s line %d", path, l.Num)
			}
			lines[l.Num] += l.Count
		}
	}

	p := &Profile{Files: make([]File, 0, len(files))}
	for path, lines := range files {
		if len(lines) == 0 {
			continue
		}
		p.Files = append(p.Files, File{Path: path, Blocks: blocksFromLineHits(lines)})
	}
	if len(p.Files) == 0 {
		return nil, errors.New("clover: no line coverage data found")
	}
	slices.SortFunc(p.Files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })
	return p, nil
}
