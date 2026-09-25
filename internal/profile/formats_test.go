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
	if _, ok := Lookup("opencover"); ok {
		t.Error("Lookup found an unknown format")
	}
}
