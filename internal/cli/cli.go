// Package cli is spin-ssh: it picks a SpinupWP site and SSHes to it as the site user,
// first granting your team SSH key to that site user through the SpinupWP API if it
// doesn't have it yet.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/marcossantiago/spin-ssh/internal/spinupwp"
	"github.com/marcossantiago/spin-ssh/internal/store"
	"github.com/marcossantiago/spin-ssh/internal/ui"
)

const usage = `spin-ssh %s: SSH to a SpinupWP site as its site user.

Usage:
  spin-ssh [flags] [query] [-- ssh args...]   pick a site and connect
  spin-ssh revoke [query]                     remove your key from a site user
  spin-ssh key                                choose which SSH key to use again
  spin-ssh token                              save a new SpinupWP API token
  spin-ssh update                             update to the latest release

Flags:
  -r, --refresh   reload servers and sites from SpinupWP before picking
  -p, --print     print the ssh command instead of running it
  -v, --version   print the version

With a query that matches exactly one site, the picker is skipped.
The SpinupWP API token comes from SPINUPWP_API_TOKEN, or is asked for and saved on
first run. Config: ` + "~/.config/spin-ssh, cache: ~/.cache/spin-ssh.\n"

type options struct {
	command string // "", "revoke", "key", "token" or "update"
	query   string
	refresh bool
	print   bool
	sshArgs []string
}

// Main runs spin-ssh. release is the version stamped into release builds, or "".
func Main(release string) {
	version = release
	ui.Version = Version()
	startUpdateCheck()
	opts, err := parseArgs(os.Args[1:])
	if err == nil {
		err = run(opts)
	}
	if errors.Is(err, ui.ErrCancelled) {
		os.Exit(130)
	}
	if errors.Is(err, errReported) {
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "spin-ssh %s: %v\n", Version(), err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (options, error) {
	var opts options
	var words []string
loop:
	for i, a := range args {
		switch a {
		case "--":
			opts.sshArgs = args[i+1:]
			break loop
		case "-r", "--refresh":
			opts.refresh = true
		case "-p", "--print":
			opts.print = true
		case "-h", "--help", "help":
			fmt.Printf(usage, Version())
			os.Exit(0)
		case "-v", "--version", "version":
			if ui.UpdateNotice != "" {
				fmt.Printf("spin-ssh %s (%s)\n", Version(), ui.UpdateNotice)
			} else {
				fmt.Println("spin-ssh", Version())
			}
			os.Exit(0)
		default:
			if strings.HasPrefix(a, "-") {
				return opts, fmt.Errorf("unknown flag %s (see spin-ssh --help)", a)
			}
			words = append(words, a)
		}
	}
	if len(words) > 0 && slices.Contains([]string{"revoke", "key", "token", "update"}, words[0]) {
		opts.command, words = words[0], words[1:]
	}
	opts.query = strings.Join(words, " ")
	return opts, nil
}

func run(opts options) error {
	switch opts.command {
	case "update":
		return runUpdate()
	case "token":
		_, err := setupToken()
		return err
	}

	token, fromFile, err := resolveToken()
	if err != nil {
		return err
	}
	err = runWithToken(&session{client: spinupwp.New(token), token: token}, opts)
	if spinupwp.IsUnauthorized(err) {
		if fromFile {
			return fmt.Errorf("%w\nYour saved token was rejected; run spin-ssh token to replace it", err)
		}
		return fmt.Errorf("%w\n%s was rejected; set it to a valid SpinupWP API token", err, tokenEnv)
	}
	return err
}

// errReported is returned after the problem was already explained to the user.
var errReported = errors.New("reported")

// session is the API client and the token it uses.
type session struct {
	client *spinupwp.Client
	token  string
}

// readOnly reports whether SpinupWP refused a change made with this token before.
func (s *session) readOnly() bool { return store.IsReadOnly(s.token) }

func runWithToken(s *session, opts options) error {
	cfg, err := ensureKey(s.client, opts.command == "key")
	if err != nil || opts.command == "key" {
		return err
	}

	if opts.command == "revoke" {
		return revoke(s, cfg, opts)
	}

	site, err := pickSite(s, cfg, opts, "spin-ssh", nil)
	if err != nil || site == nil {
		return err
	}
	if err := grant(s, cfg, site); err != nil {
		return err
	}
	return connect(cfg, site, opts)
}

// connect replaces this process with ssh, pinned to the chosen key.
func connect(cfg *store.Config, site *store.Site, opts options) error {
	port := site.Port
	if port == 0 {
		port = 22
	}
	args := []string{
		"ssh",
		"-i", cfg.IdentityFile,
		"-o", "IdentitiesOnly=yes",
		"-p", strconv.Itoa(port),
		site.SiteUser + "@" + site.Host,
	}
	args = append(args, opts.sshArgs...)

	if opts.print {
		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = shellQuote(a)
		}
		fmt.Println(strings.Join(quoted, " "))
		return nil
	}

	path, err := exec.LookPath("ssh")
	if err != nil {
		return err
	}
	_ = store.TouchRecent(site.ID)
	ui.Note("Connecting to %s on %s as %s", site.Domain, site.ServerName, site.SiteUser)
	return syscall.Exec(path, args, os.Environ())
}

// grant adds the user's team key to the site user unless the cache says it's there.
// With a read-only token it explains how to add the key in SpinupWP instead.
func grant(s *session, cfg *store.Config, site *store.Site) error {
	if site.HasKey(cfg.SSHKeyID) {
		return nil
	}
	if site.UserAuth == "password" {
		return fmt.Errorf("%s's site user %s uses password authentication; SpinupWP can only add SSH keys to site users with publickey authentication (change it in the site's settings)", site.Domain, site.SiteUser)
	}
	if s.readOnly() {
		return grantByHand(s, cfg, site)
	}

	title := fmt.Sprintf("Adding your key %q to %s on %s…", cfg.SSHKeyName, site.SiteUser, site.Domain)
	err := ui.Run(title, func(ctx context.Context) error {
		id, err := s.client.AddSiteUserKey(ctx, site.ID, cfg.SSHKeyID)
		if err != nil {
			return err
		}
		return s.client.WaitForEvent(ctx, id)
	})
	switch {
	case spinupwp.IsForbidden(err):
		_ = store.MarkReadOnly(s.token)
		return grantByHand(s, cfg, site)
	case spinupwp.IsNotFound(err):
		return fmt.Errorf("%s no longer exists in SpinupWP, run spin-ssh --refresh", site.Domain)
	case err != nil:
		return err
	}
	ui.Success("Added your key %q to %s", cfg.SSHKeyName, site.SiteUser)
	site.Keys = append(site.Keys, store.Key{ID: cfg.SSHKeyID, Name: cfg.SSHKeyName})
	return store.UpdateSite(*site)
}

// grantByHand is grant for a read-only token: the user adds the key in SpinupWP.
// The key may already be there (added since the cache was filled), so it checks the
// site first. Otherwise it links to the site's SFTP settings and waits: enter checks
// again and connects once the key is there.
func grantByHand(s *session, cfg *store.Config, site *store.Site) error {
	if err := refreshSiteKeys(s, site); err != nil {
		return err
	}
	if site.HasKey(cfg.SSHKeyID) {
		return nil
	}

	url := siteSFTPURL(site.ID)
	tty := term.IsTerminal(int(os.Stdin.Fd()))
	last := "Come back here and press enter"
	if !tty {
		last = "Run spin-ssh again"
	}
	printSteps(fmt.Sprintf("🔒 Read-only token: add your key to %s in SpinupWP", site.Domain),
		openStep(url, site),
		fmt.Sprintf("Add the SSH key %q to site user %s", cfg.SSHKeyName, site.SiteUser),
		last)
	if !tty {
		return errReported
	}

	keys := []ui.Key{{Key: "enter", Does: "check again"}, {Key: "c", Does: "connect anyway"}, {Key: "esc", Does: "quit"}}
	if url != "" {
		keys = append([]ui.Key{{Key: "o", Does: "open link"}}, keys...)
	}
	for {
		k, err := ui.Keys(keys...)
		if err != nil {
			return err
		}
		switch k {
		case "o":
			if openBrowser(url) == nil {
				fmt.Fprintln(os.Stderr, "   🌐 Opened in your browser")
			}
		case "enter":
			if err := refreshSiteKeys(s, site); err != nil {
				return err
			}
			if site.HasKey(cfg.SSHKeyID) {
				fmt.Fprintf(os.Stderr, "   ✅ Your key is on %s\n", site.Domain)
				return nil
			}
			fmt.Fprintln(os.Stderr, "   ⏳ Not there yet. SpinupWP can take a minute to apply it.")
		case "c":
			return nil
		default:
			return errReported
		}
	}
}

// refreshSiteKeys reloads the keys on one site and updates the cache.
func refreshSiteKeys(s *session, site *store.Site) error {
	var fresh *spinupwp.Site
	err := ui.Run(fmt.Sprintf("Checking whether your key is on %s…", site.Domain), func(ctx context.Context) error {
		var err error
		fresh, err = s.client.GetSite(ctx, site.ID)
		return err
	})
	if err != nil {
		return err
	}
	site.Keys = storeKeys(fresh.SSHKeys)
	return store.UpdateSite(*site)
}

// printSteps prints a heading and numbered steps, for things done in SpinupWP.
func printSteps(heading string, steps ...string) {
	fmt.Fprintf(os.Stderr, "\n%s\n\n", heading)
	for i, step := range steps {
		fmt.Fprintf(os.Stderr, "   %d. %s\n", i+1, step)
	}
	fmt.Fprintln(os.Stderr)
}

// openStep is the first step: the direct link when the organization is known.
func openStep(url string, site *store.Site) string {
	if url != "" {
		return "Open " + url
	}
	return fmt.Sprintf("Open https://spinupwp.app, then the site %s and its SFTP tab", site.Domain)
}

func revoke(s *session, cfg *store.Config, opts options) error {
	hasKey := func(site store.Site) bool { return site.HasKey(cfg.SSHKeyID) }
	site, err := pickSite(s, cfg, opts, "spin-ssh revoke", hasKey)
	if err != nil || site == nil {
		return err
	}
	if !s.readOnly() {
		title := fmt.Sprintf("Removing your key %q from %s on %s…", cfg.SSHKeyName, site.SiteUser, site.Domain)
		err = ui.Run(title, func(ctx context.Context) error {
			id, err := s.client.RemoveSiteUserKey(ctx, site.ID, cfg.SSHKeyID)
			if err != nil {
				return err
			}
			return s.client.WaitForEvent(ctx, id)
		})
	}
	if s.readOnly() || spinupwp.IsForbidden(err) {
		_ = store.MarkReadOnly(s.token)
		url := siteSFTPURL(site.ID)
		printSteps(fmt.Sprintf("🔒 Read-only token: remove your key from %s in SpinupWP", site.Domain),
			openStep(url, site),
			fmt.Sprintf("Remove the SSH key %q from site user %s", cfg.SSHKeyName, site.SiteUser))
		if url != "" && term.IsTerminal(int(os.Stdin.Fd())) {
			if k, _ := ui.Keys(ui.Key{Key: "o", Does: "open link"}, ui.Key{Key: "esc", Does: "quit"}); k == "o" {
				_ = openBrowser(url)
			}
		}
		return errReported
	}
	if err != nil {
		return err
	}
	ui.Success("Removed your key %q from %s on %s", cfg.SSHKeyName, site.SiteUser, site.Domain)
	var keys []store.Key
	for _, k := range site.Keys {
		if k.ID != cfg.SSHKeyID {
			keys = append(keys, k)
		}
	}
	site.Keys = keys
	return store.UpdateSite(*site)
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=@:,+%", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
