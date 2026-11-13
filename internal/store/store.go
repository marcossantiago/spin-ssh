// Package store keeps spin-ssh's config (which key to use) and its cache of sites.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Config is the user's chosen identity, saved on first run.
type Config struct {
	// SSHKeyID and SSHKeyName are the matching key on the SpinupWP team.
	SSHKeyID   int64  `json:"ssh_key_id"`
	SSHKeyName string `json:"ssh_key_name"`
	// PublicKey is the authorized_keys line, used to check the choice is still valid.
	PublicKey string `json:"public_key"`
	// IdentityFile is passed to ssh -i: a private key, or a .pub file when the private
	// key only lives in an agent (ssh then asks the agent for that key).
	IdentityFile string `json:"identity_file"`
}

// Site is a site joined with its server: everything needed to connect.
type Site struct {
	ID         int64  `json:"id"`
	Domain     string `json:"domain"`
	SiteUser   string `json:"site_user"`
	UserAuth   string `json:"user_auth"`
	PHPVersion string `json:"php_version"`
	ServerID   int64  `json:"server_id"`
	ServerName string `json:"server_name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Keys       []Key  `json:"keys"`
}

// Key is a team SSH key granted to the site user.
type Key struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// HasKey reports whether the team key id is granted to the site user.
func (s Site) HasKey(id int64) bool {
	return slices.ContainsFunc(s.Keys, func(k Key) bool { return k.ID == id })
}

// Inventory is the cached list of sites.
type Inventory struct {
	FetchedAt time.Time `json:"fetched_at"`
	Sites     []Site    `json:"sites"`
}

// ConfigDir is $XDG_CONFIG_HOME/spin-ssh, or ~/.config/spin-ssh.
func ConfigDir() string { return xdgDir("XDG_CONFIG_HOME", ".config") }

// CacheDir is $XDG_CACHE_HOME/spin-ssh, or ~/.cache/spin-ssh.
func CacheDir() string { return xdgDir("XDG_CACHE_HOME", ".cache") }

func xdgDir(env, fallback string) string {
	base := os.Getenv(env)
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, fallback)
	}
	return filepath.Join(base, "spin-ssh")
}

// LoadConfig returns the saved config, or nil when there is none yet.
func LoadConfig() (*Config, error) {
	var c Config
	ok, err := readJSON(filepath.Join(ConfigDir(), "config.json"), &c)
	if !ok {
		return nil, err
	}
	return &c, nil
}

// SaveConfig writes the config.
func SaveConfig(c *Config) error {
	return writeJSON(filepath.Join(ConfigDir(), "config.json"), c)
}

// LoadInventory returns the cached sites, or nil when there is no cache.
func LoadInventory() (*Inventory, error) {
	var inv Inventory
	ok, err := readJSON(filepath.Join(CacheDir(), "inventory.json"), &inv)
	if !ok {
		return nil, err
	}
	return &inv, nil
}

// SaveInventory writes the cache.
func SaveInventory(inv *Inventory) error {
	return writeJSON(filepath.Join(CacheDir(), "inventory.json"), inv)
}

// UpdateSite replaces one site in the cache, keeping the rest.
func UpdateSite(site Site) error {
	inv, err := LoadInventory()
	if err != nil || inv == nil {
		return err
	}
	for i := range inv.Sites {
		if inv.Sites[i].ID == site.ID {
			inv.Sites[i] = site
		}
	}
	return SaveInventory(inv)
}

func readJSON(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		// A corrupt file is treated as missing: config is redone, cache refetched.
		return false, nil
	}
	return true, nil
}

// writeJSON writes atomically, so a run killed mid-write (spin-ssh execs ssh while a
// background refresh may still be going) never leaves a half-written file.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// LoadRecent returns when each site was last connected to, by site ID.
func LoadRecent() map[int64]time.Time {
	recent := map[int64]time.Time{}
	_, _ = readJSON(filepath.Join(CacheDir(), "recent.json"), &recent)
	return recent
}

// TouchRecent records a connection to the site now.
func TouchRecent(id int64) error {
	recent := LoadRecent()
	recent[id] = time.Now()
	return writeJSON(filepath.Join(CacheDir(), "recent.json"), recent)
}

// TokenFile is where the API token is saved when it isn't in the environment.
func TokenFile() string { return filepath.Join(ConfigDir(), "credentials.json") }

// LoadToken returns the saved API token, or "".
func LoadToken() string {
	var c struct {
		Token string `json:"spinupwp_api_token"`
	}
	_, _ = readJSON(TokenFile(), &c)
	return c.Token
}

// SaveToken saves the API token, readable only by the user.
func SaveToken(token string) error {
	return writeJSON(TokenFile(), map[string]string{"spinupwp_api_token": token})
}

// UpdateState is the last check for a new release.
type UpdateState struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// LoadUpdateState returns the last update check; zero if there was none.
func LoadUpdateState() UpdateState {
	var s UpdateState
	_, _ = readJSON(filepath.Join(CacheDir(), "update.json"), &s)
	return s
}

// SaveUpdateState records an update check.
func SaveUpdateState(s UpdateState) error {
	return writeJSON(filepath.Join(CacheDir(), "update.json"), s)
}
