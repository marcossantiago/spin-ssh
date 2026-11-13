package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/marcossantiago/spin-ssh/internal/store"
	"github.com/marcossantiago/spin-ssh/internal/ui"
	"github.com/marcossantiago/spin-ssh/internal/update"
)

// checkEvery is how often a release build looks for a new release, in the background.
const checkEvery = 24 * time.Hour

// startUpdateCheck shows the last check's result and, at most once a day, checks
// again in the background for the next run. Dev builds don't check.
func startUpdateCheck() {
	if os.Getenv("SPIN_SSH_NO_UPDATE_CHECK") != "" || !update.IsRelease(Version()) {
		return
	}
	last := store.LoadUpdateState()
	if update.Newer(Version(), last.Latest) {
		ui.UpdateNotice = last.Latest + " available: spin-ssh update"
	}
	if time.Since(last.CheckedAt) < checkEvery {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		latest, err := update.New().Latest(ctx)
		if err != nil {
			latest = last.Latest // try again tomorrow, not on every run
		}
		_ = store.SaveUpdateState(store.UpdateState{CheckedAt: time.Now(), Latest: latest})
	}()
}

// runUpdate replaces this binary with the latest release, or says how to update it
// when go install or Homebrew installed it.
func runUpdate() error {
	exe, err := update.Executable()
	if err != nil {
		return err
	}
	u := update.New()
	var latest string
	err = ui.Run("Checking for a new release…", func(ctx context.Context) error {
		var err error
		latest, err = u.Latest(ctx)
		return err
	})
	if err != nil {
		return err
	}
	_ = store.SaveUpdateState(store.UpdateState{CheckedAt: time.Now(), Latest: latest})
	if !update.Newer(Version(), latest) {
		ui.Success("spin-ssh %s is the latest version", Version())
		return nil
	}

	switch update.Detect(exe) {
	case update.GoInstall:
		ui.Note("spin-ssh %s is available. You installed it with go install, so update with:", latest)
		fmt.Fprintf(os.Stderr, "\n  go install github.com/%s@latest\n\n", update.Repo)
		return nil
	case update.Homebrew:
		ui.Note("spin-ssh %s is available. You installed it with Homebrew, so update with:", latest)
		fmt.Fprint(os.Stderr, "\n  brew upgrade spin-ssh\n\n")
		return nil
	}

	err = ui.Run(fmt.Sprintf("Downloading spin-ssh %s…", latest), func(ctx context.Context) error {
		return u.Apply(ctx, latest, exe)
	})
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("%w\nReinstall to a folder you can write to with the install script (it uses ~/.local/bin)", err)
	}
	if err != nil {
		return err
	}
	ui.Success("Updated spin-ssh %s → %s", Version(), latest)
	return nil
}
