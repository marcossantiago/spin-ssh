package cli

import (
	"slices"
	"testing"
)

func TestParseArgs(t *testing.T) {
	opts, err := parseArgs([]string{"-r", "staging", "flow", "--", "-L", "3306:localhost:3306"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.refresh || opts.query != "staging flow" || !slices.Equal(opts.sshArgs, []string{"-L", "3306:localhost:3306"}) {
		t.Fatalf("opts = %+v", opts)
	}

	opts, _ = parseArgs([]string{"revoke", "docs"})
	if opts.command != "revoke" || opts.query != "docs" {
		t.Fatalf("revoke opts = %+v", opts)
	}

	if _, err := parseArgs([]string{"--nope"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"labs@1.2.3.4":       "labs@1.2.3.4",
		"/Users/x/.ssh/id":   "/Users/x/.ssh/id",
		"/Users/x/My Keys/k": "'/Users/x/My Keys/k'",
		"it's":               `'it'\''s'`,
		"":                   "''",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
