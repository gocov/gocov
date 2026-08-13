package main

import (
	"strings"
	"testing"
)

func TestSecretKey(t *testing.T) {
	const valid = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "unset", raw: "", want: ""},
		{name: "blank is treated as unset", raw: "   \n\t ", want: ""},
		{name: "valid lowercase", raw: valid, want: valid},
		{name: "valid uppercase", raw: strings.ToUpper(valid), want: strings.ToUpper(valid)},
		{name: "trims surrounding whitespace", raw: "  " + valid + "\n", want: valid},
		{name: "passphrase rejected", raw: "hunter2", wantErr: true},
		{name: "too short", raw: valid[:63], wantErr: true},
		{name: "too long", raw: valid + "0", wantErr: true},
		{name: "non-hex char", raw: valid[:63] + "g", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := secretKey(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("secretKey(%q) = %q, nil; want error", tc.raw, got)
				}
				// The message must name the variable and how to generate one.
				if !strings.Contains(err.Error(), "64 hex characters") ||
					!strings.Contains(err.Error(), "openssl rand -hex 32") {
					t.Errorf("error %q missing guidance", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("secretKey(%q): unexpected error %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("secretKey(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
