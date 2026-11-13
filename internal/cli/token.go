package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/marcossantiago/spin-ssh/internal/spinupwp"
	"github.com/marcossantiago/spin-ssh/internal/store"
	"github.com/marcossantiago/spin-ssh/internal/ui"
)

const tokenEnv = "SPINUPWP_API_TOKEN"

// apiSettingsURL is where an organization's API tokens are created.
func apiSettingsURL(org string) string {
	if org == "" {
		return "https://spinupwp.app/{ORGANIZATION_SLUG}/settings/api"
	}
	return "https://spinupwp.app/" + org + "/settings/api"
}

// siteSFTPURL links to a site's SFTP settings, where its site user's SSH keys are, or
// is "" when the organization isn't known.
func siteSFTPURL(siteID int64) string {
	org := store.LoadCredentials().Organization
	if org == "" {
		return ""
	}
	return fmt.Sprintf("https://spinupwp.app/%s/sites/%d#sftp", org, siteID)
}

// parseOrganization accepts a slug or any SpinupWP URL and returns the slug.
func parseOrganization(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	s = strings.TrimPrefix(s, "spinupwp.app")
	s = strings.Trim(s, "/")
	slug, _, _ := strings.Cut(s, "/")
	return slug
}

// resolveToken returns the API token from the environment, else the saved one, else
// asks for one and saves it. fromFile reports whether it came from the saved file.
func resolveToken() (token string, fromFile bool, err error) {
	if t := strings.TrimSpace(os.Getenv(tokenEnv)); t != "" {
		return t, false, nil
	}
	if t := store.LoadCredentials().Token; t != "" {
		return t, true, nil
	}
	t, err := setupToken()
	return t, true, err
}

// setupToken asks for the organization (once) and a token, offering to open the
// organization's API settings, then checks the token and saves both.
func setupToken() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("no SpinupWP API token: set " + tokenEnv + ", or run spin-ssh token in a terminal")
	}
	creds := store.LoadCredentials()

	ui.Note("spin-ssh needs a SpinupWP API token from your organization's Settings → API page.")
	ui.Note("A read-only token is enough to list sites and connect. A read-write token also lets")
	ui.Note("spin-ssh add your SSH key to a site for you; with read-only you add it in SpinupWP.")

	if creds.Organization == "" {
		org, err := ui.Ask("Your SpinupWP organization: paste any SpinupWP URL, or the part after spinupwp.app/", "my-team")
		if err != nil {
			return "", err
		}
		creds.Organization = parseOrganization(org)
	}
	url := apiSettingsURL(creds.Organization)
	if creds.Organization == "" {
		ui.Note("Open https://spinupwp.app, pick your organization, then Settings → API.")
		ui.Note("The page's address looks like %s", url)
	} else {
		open, err := ui.Confirm("Open "+url+" in your browser?", true)
		if err != nil {
			return "", err
		}
		if !open || openBrowser(url) != nil {
			ui.Note("Create a token at %s", url)
		}
	}

	for range 3 {
		token, err := ui.AskSecret("Paste your API token:")
		if err != nil {
			return "", err
		}
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		err = ui.Run("Checking the token with SpinupWP…", func(ctx context.Context) error {
			return spinupwp.New(token).Check(ctx)
		})
		if spinupwp.IsUnauthorized(err) || spinupwp.IsForbidden(err) {
			ui.Warn("SpinupWP rejected that token. Try again.")
			continue
		}
		if err != nil {
			return "", err
		}
		creds.Token = token
		if err := store.SaveCredentials(creds); err != nil {
			return "", err
		}
		ui.Success("Saved your token to %s", tildePath(store.TokenFile()))
		if os.Getenv(tokenEnv) != "" {
			ui.Warn("%s is set and takes priority over the saved token.", tokenEnv)
		}
		return token, nil
	}
	return "", errors.New("no valid SpinupWP API token")
}

func openBrowser(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Run()
}

func tildePath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+"/") {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
