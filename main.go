// spin-ssh picks a SpinupWP site and SSHes to it as the site user, first granting your
// team SSH key to that site user through the SpinupWP API if it doesn't have it yet.
package main

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

func main() {
	ui.Version = Version()
	startUpdateCheck()
	opts, err := parseArgs(os.Args[1:])
	if err == nil {
		err = run(opts)
	}
	if errors.Is(err, ui.ErrCancelled) {
		os.Exit(130)
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
	err = runWithToken(spinupwp.New(token), opts)
	if spinupwp.IsUnauthorized(err) {
		if fromFile {
			return fmt.Errorf("%w\nYour saved token was rejected; run spin-ssh token to replace it", err)
		}
		return fmt.Errorf("%w\n%s was rejected; set it to a valid SpinupWP API token", err, tokenEnv)
	}
	return err
}

func runWithToken(client *spinupwp.Client, opts options) error {
	cfg, err := ensureKey(client, opts.command == "key")
	if err != nil || opts.command == "key" {
		return err
	}

	if opts.command == "revoke" {
		return revoke(client, cfg, opts)
	}

	site, err := pickSite(client, cfg, opts, "spin-ssh", nil)
	if err != nil || site == nil {
		return err
	}
	if err := grant(client, cfg, site); err != nil {
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
func grant(client *spinupwp.Client, cfg *store.Config, site *store.Site) error {
	if site.HasKey(cfg.SSHKeyID) {
		return nil
	}
	if site.UserAuth == "password" {
		return fmt.Errorf("%s's site user %s uses password authentication; SpinupWP can only add SSH keys to site users with publickey authentication (change it in the site's settings)", site.Domain, site.SiteUser)
	}
	title := fmt.Sprintf("Adding your key %q to %s on %s…", cfg.SSHKeyName, site.SiteUser, site.Domain)
	err := ui.Run(title, func(ctx context.Context) error {
		id, err := client.AddSiteUserKey(ctx, site.ID, cfg.SSHKeyID)
		if err != nil {
			return err
		}
		return client.WaitForEvent(ctx, id)
	})
	if spinupwp.IsNotFound(err) {
		return fmt.Errorf("%s no longer exists in SpinupWP, run spin-ssh --refresh", site.Domain)
	}
	if err != nil {
		return err
	}
	ui.Success("Added your key %q to %s", cfg.SSHKeyName, site.SiteUser)
	site.Keys = append(site.Keys, store.Key{ID: cfg.SSHKeyID, Name: cfg.SSHKeyName})
	return store.UpdateSite(*site)
}

func revoke(client *spinupwp.Client, cfg *store.Config, opts options) error {
	hasKey := func(s store.Site) bool { return s.HasKey(cfg.SSHKeyID) }
	site, err := pickSite(client, cfg, opts, "spin-ssh revoke", hasKey)
	if err != nil || site == nil {
		return err
	}
	title := fmt.Sprintf("Removing your key %q from %s on %s…", cfg.SSHKeyName, site.SiteUser, site.Domain)
	err = ui.Run(title, func(ctx context.Context) error {
		id, err := client.RemoveSiteUserKey(ctx, site.ID, cfg.SSHKeyID)
		if err != nil {
			return err
		}
		return client.WaitForEvent(ctx, id)
	})
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
