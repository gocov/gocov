// Package config declares every environment variable the gocov binaries
// read. Each binary gets one struct, in its own file here — Server, CLI,
// Preview — with each field tagged with the variable it comes from,
// parsed once at start-up and validated before anything is wired
// together.
//
// The point is that the environment contract lives in a single readable
// place instead of being spread over os.Getenv calls at the point of use:
// these structs are the authoritative list of variables, and the doc
// comments on the binaries plus docs/configuration.md mirror them for
// users. Parsing from an explicit map also makes the whole contract
// testable without touching the process environment.
//
// Tags cover reading, defaults, types and presence. Everything the tag
// vocabulary cannot say — the shape of a value, or a rule spanning
// several variables — stays as ordinary Go beside the struct it governs,
// split by whether it is fatal (validate) or survivable (Warnings).
package config

import (
	env "github.com/caarlos0/env/v11"
)

// parse fills T from environ, or from the process environment when
// environ is nil. Every Load function in this package goes through here,
// so that nil-means-the-process rule is stated once.
func parse[T any](environ map[string]string) (T, error) {
	return env.ParseAsWithOptions[T](env.Options{Environment: environ})
}

// OAuthApp is one forge's OAuth credentials. The two halves are read
// under a per-forge prefix (GOCOV_OAUTH_GITHUB_ and friends), so the
// field tags here are just the KEY/SECRET suffixes.
type OAuthApp struct {
	Key    string `env:"KEY"`
	Secret string `env:"SECRET"`
}

// Configured reports whether the app is usable: both halves present.
func (a OAuthApp) Configured() bool { return a.Key != "" && a.Secret != "" }

// Partial reports whether exactly one half was set, which is always a
// mistake — the caller warns and leaves the forge switched off.
func (a OAuthApp) Partial() bool { return !a.Configured() && (a.Key != "" || a.Secret != "") }
