package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/marcossantiago/spin-ssh/internal/spinupwp"
	"github.com/marcossantiago/spin-ssh/internal/store"
	"github.com/marcossantiago/spin-ssh/internal/ui"
)

// tokenPageURL is opened so the user can create a token. SpinupWP's docs don't link
// the API Tokens tab directly, so this is the app; the prompt says where to click.
const tokenPageURL = "https://spinupwp.app"

const tokenEnv = "SPINUPWP_API_TOKEN"

// resolveToken returns the API token from the environment, else the saved one, else
// asks for one and saves it. fromFile reports whether it came from the saved file.
func resolveToken() (token string, fromFile bool, err error) {
	if t := strings.TrimSpace(os.Getenv(tokenEnv)); t != "" {
		return t, false, nil
	}
	if t := store.LoadToken(); t != "" {
		return t, true, nil
	}
	t, err := setupToken()
	return t, true, err
}

// setupToken asks for a token, offering to open SpinupWP, checks it and saves it.
func setupToken() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("no SpinupWP API token: set " + tokenEnv + ", or run spin-ssh token in a terminal")
	}
	ui.Note("spin-ssh needs a SpinupWP API token. Create one in SpinupWP under My Account → API Tokens")
	ui.Note("(or Team Settings → API Tokens for a team token).")
	open, err := ui.Confirm("Open SpinupWP in your browser?", true)
	if err != nil {
		return "", err
	}
	if open {
		if err := openBrowser(tokenPageURL); err != nil {
			ui.Warn("Couldn't open a browser. Go to %s", tokenPageURL)
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
		if spinupwp.IsUnauthorized(err) {
			ui.Warn("SpinupWP rejected that token. Try again.")
			continue
		}
		if err != nil {
			return "", err
		}
		if err := store.SaveToken(token); err != nil {
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
