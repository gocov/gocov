package profile

import "testing"

func TestEveryDetectableFormatIsRegistered(t *testing.T) {
	// The upload API dispatches on these names, and Detect hands them
	// out; dropping one silently turns every upload of that format into
	// a 400.
	for _, name := range []string{"go", "lcov", "jacoco", "cobertura", "clover", "simplecov"} {
		f, ok := Lookup(name)
		if !ok || f.Parser == nil || f.Name != name {
			t.Errorf("Lookup(%q) = %+v, %v; want a registered parser", name, f, ok)
		}
	}
	if got := Names(); len(got) != len(formats) || got[0] != "go" {
		t.Errorf("Names() = %v", got)
	}
	if _, ok := Lookup("opencover"); ok {
		t.Error("Lookup found an unknown format")
	}
}

// Every format names its download and says which changed files are source;
// a format left without either falls back to a made-up name and flags
// READMEs as untested code.
func TestEveryFormatNamesItsFilesAndSources(t *testing.T) {
	for _, f := range formats {
		if f.Filename == "" || len(f.SourceExts) == 0 {
			t.Errorf("%s: filename %q, source exts %v; want both", f.Name, f.Filename, f.SourceExts)
		}
	}
}
