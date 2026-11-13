package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/marcossantiago/spin-ssh/internal/spinupwp"
	"github.com/marcossantiago/spin-ssh/internal/store"
)

// fakeReadOnlyAPI refuses changes like a read-only token. GET /sites/7 lists keyOnSite.
func fakeReadOnlyAPI(t *testing.T, keyOnSite int64, posts *atomic.Int32) *session {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sites/7/site-user/ssh-keys", func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message": "This action is unauthorized."}`)
	})
	mux.HandleFunc("GET /sites/7", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data": {"id": 7, "domain": "shop.example.com", "ssh_keys": [{"id": %d, "name": "k"}]}}`, keyOnSite)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := spinupwp.New("read-only-token")
	c.BaseURL = srv.URL
	return &session{client: c, token: "read-only-token"}
}

func TestGrantWithReadOnlyToken(t *testing.T) {
	var posts atomic.Int32
	s := fakeReadOnlyAPI(t, 99, &posts)
	cfg := &store.Config{SSHKeyID: 1, SSHKeyName: "Me"}
	site := &store.Site{ID: 7, Domain: "shop.example.com", UserAuth: "publickey"}

	// The key isn't on the site: explain and stop (no terminal to ask "try anyway?").
	if err := grant(s, cfg, site); err != errReported {
		t.Fatalf("grant = %v, want errReported", err)
	}
	if !s.readOnly() {
		t.Fatal("token not remembered as read-only")
	}
	// Next time it doesn't try to change anything.
	_ = grant(s, cfg, site)
	if n := posts.Load(); n != 1 {
		t.Fatalf("%d POSTs, want 1", n)
	}
}

func TestGrantWithReadOnlyTokenAfterKeyAddedInSpinupWP(t *testing.T) {
	var posts atomic.Int32
	s := fakeReadOnlyAPI(t, 1, &posts) // the user added key 1 in SpinupWP
	_ = store.MarkReadOnly(s.token)
	cfg := &store.Config{SSHKeyID: 1, SSHKeyName: "Me"}
	site := &store.Site{ID: 7, Domain: "shop.example.com", UserAuth: "publickey"}
	if err := grant(s, cfg, site); err != nil {
		t.Fatalf("grant = %v, want nil (connect)", err)
	}
	if !site.HasKey(1) || posts.Load() != 0 {
		t.Fatalf("site keys %v, %d POSTs", site.Keys, posts.Load())
	}
}

func TestParseOrganization(t *testing.T) {
	for in, want := range map[string]string{
		"my-team": "my-team",
		" https://spinupwp.app/my-team/servers/1": "my-team",
		"spinupwp.app/my-team/settings/api":       "my-team",
		"https://spinupwp.app/":                   "",
	} {
		if got := parseOrganization(in); got != want {
			t.Errorf("parseOrganization(%q) = %q, want %q", in, got, want)
		}
	}
}
