package server

// PostHog configures the opt-in browser analytics snippet. The server
// hands the key and host to the app in its session response (api.go) and
// the app initialises posthog-js from them; nothing here runs
// server-side, and nothing loads in the browser unless Key is set. The
// snippet is deliberately narrow: memory persistence (no cookie, no
// localStorage), no autocapture, Do Not Track honoured, and session
// replay only on the sign-in and setup screens with every input masked
// and the upload token blocked — never on a report or source view.
// Signed-in users are identified by their gocov user id, never by email,
// so what PostHog holds is a numeric pseudonym, page paths and the
// setup-flow recordings.
type PostHog struct {
	// Key is the PostHog project API key (phc_...). It is public by
	// design — every visitor's browser sees it — so it is not a secret.
	Key string
	// Host is the ingestion endpoint the browser loads posthog-js from
	// and sends events to, e.g. https://eu.i.posthog.com.
	Host string
}

// Configured reports whether the snippet loads at all.
func (p PostHog) Configured() bool { return p.Key != "" }
