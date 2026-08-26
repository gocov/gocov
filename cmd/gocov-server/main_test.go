package main

import "testing"

func TestHealthURLDialsLoopbackForWildcardListeners(t *testing.T) {
	for addr, want := range map[string]string{
		":8080":               "http://127.0.0.1:8080/healthz",
		"8080":                "http://127.0.0.1:8080/healthz",
		"0.0.0.0:8080":        "http://127.0.0.1:8080/healthz",
		"[::]:9000":           "http://127.0.0.1:9000/healthz",
		"127.0.0.1:8080":      "http://127.0.0.1:8080/healthz",
		"internal.example:80": "http://internal.example:80/healthz",
	} {
		if got := healthURL(addr); got != want {
			t.Errorf("healthURL(%q) = %q, want %q", addr, got, want)
		}
	}
}
