// Package hosted holds shared constants for the hosted gocov service, so
// the CLI's default server and the onboarding page's "server is implicit"
// check stay in sync from a single source of truth.
package hosted

// DefaultServer is the hosted gocov instance the CLI uploads to when no
// -server / $GOCOV_SERVER is given, and which the onboarding page treats
// as implicit (so hosted users only configure a token). Self-hosters run
// under a different base URL and set the server explicitly.
const DefaultServer = "https://app.gocov.dev"
