package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marcossantiago/spin-ssh/internal/spinupwp"
	"github.com/marcossantiago/spin-ssh/internal/sshkeys"
	"github.com/marcossantiago/spin-ssh/internal/store"
	"github.com/marcossantiago/spin-ssh/internal/ui"
)

// ensureKey returns the saved key choice, or works it out on first run (or when
// forced, or when the saved identity file is gone): match local keys against the team's.
func ensureKey(client *spinupwp.Client, force bool) (*store.Config, error) {
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg != nil && !force {
		if _, err := os.Stat(cfg.IdentityFile); err == nil {
			return cfg, nil
		}
		fmt.Fprintf(os.Stderr, "Your saved key %s is gone, looking again.\n", cfg.IdentityFile)
	}

	var team []spinupwp.SSHKey
	var local []sshkeys.Local
	err = ui.Run("Matching your SSH keys with the SpinupWP team's…", func(ctx context.Context) error {
		var err error
		if team, err = client.ListSSHKeys(ctx); err != nil {
			return err
		}
		local, err = sshkeys.Discover()
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(team) == 0 {
		return nil, errors.New("the SpinupWP team has no SSH keys; ask an admin to add yours in SpinupWP > Team Settings > SSH Keys")
	}

	var chosen sshkeys.Match
	switch matches := sshkeys.MatchTeam(local, team); len(matches) {
	case 1:
		chosen = matches[0]
	case 0:
		if chosen, err = pickManually(local, team); err != nil {
			return nil, err
		}
	default:
		if chosen, err = pickMatch(matches); err != nil {
			return nil, err
		}
	}

	identity, err := sshkeys.IdentityFile(chosen.Local, filepath.Join(store.ConfigDir(), "keys"))
	if err != nil {
		return nil, err
	}
	cfg = &store.Config{
		SSHKeyID:     chosen.Team.ID,
		SSHKeyName:   chosen.Team.Name,
		PublicKey:    chosen.Local.AuthorizedKey(),
		IdentityFile: identity,
	}
	if err := store.SaveConfig(cfg); err != nil {
		return nil, err
	}
	ui.Success("Using team key %q from %s. Change it with spin-ssh key.", chosen.Team.Name, chosen.Local.Describe())
	return cfg, nil
}

func pickMatch(matches []sshkeys.Match) (sshkeys.Match, error) {
	items := make([]ui.Item, len(matches))
	for i, m := range matches {
		items[i] = ui.Item{
			Key:     strconv.Itoa(i),
			Columns: []string{m.Team.Name, m.Local.Describe()},
			Title:   m.Team.Name,
			Fields:  keyFields(m.Team, &m.Local),
			Value:   m,
		}
	}
	it, err := ui.Pick(ui.PickerOptions{
		Title:   "Several of your keys are on the team: which should spin-ssh use?",
		Noun:    "key",
		Headers: []string{"Team key", "Local key"},
		Action:  "use this key",
		Items:   items,
	})
	if err != nil {
		return sshkeys.Match{}, err
	}
	if it == nil {
		return sshkeys.Match{}, ui.ErrCancelled
	}
	return it.Value.(sshkeys.Match), nil
}

// pickManually handles a key spin-ssh couldn't find: outside ~/.ssh, or in an agent
// ssh isn't set up to use. The user says which team key is theirs and where it is.
func pickManually(local []sshkeys.Local, team []spinupwp.SSHKey) (sshkeys.Match, error) {
	fmt.Fprintln(os.Stderr, "None of your SSH keys are on the SpinupWP team. Found:")
	for _, l := range local {
		fmt.Fprintf(os.Stderr, "  %s  %s\n", l.Fingerprint(), l.Describe())
	}
	if len(local) == 0 {
		fmt.Fprintln(os.Stderr, "  (no private keys in ~/.ssh and none in your SSH agent)")
	}
	fmt.Fprintln(os.Stderr, "If your key is elsewhere, pick it from the team's keys next. Otherwise ask an admin to add your public key in SpinupWP > Team Settings > SSH Keys.")

	items := make([]ui.Item, len(team))
	for i, t := range team {
		items[i] = ui.Item{
			Key:     strconv.FormatInt(t.ID, 10),
			Columns: []string{t.Name},
			Title:   t.Name,
			Fields:  keyFields(t, nil),
			Value:   t,
		}
	}
	it, err := ui.Pick(ui.PickerOptions{
		Title:       "Which team key is yours?",
		Placeholder: "Search by name",
		Noun:        "team key",
		Headers:     []string{"Name"},
		Action:      "choose",
		Items:       items,
	})
	if err != nil {
		return sshkeys.Match{}, err
	}
	if it == nil {
		return sshkeys.Match{}, ui.ErrCancelled
	}
	t := it.Value.(spinupwp.SSHKey)

	path, err := ui.Ask(fmt.Sprintf("Path to the private key for %q:", t.Name), "~/.ssh/id_ed25519")
	if err != nil {
		return sshkeys.Match{}, err
	}
	l, err := sshkeys.Verify(sshkeys.ExpandHome(strings.TrimSpace(path)), t)
	if err != nil {
		return sshkeys.Match{}, err
	}
	return sshkeys.Match{Local: l, Team: t}, nil
}

func keyFields(t spinupwp.SSHKey, l *sshkeys.Local) []ui.Field {
	fields := []ui.Field{{Label: "MD5", Value: t.Fingerprint, Tone: ui.ToneDim}}
	if l != nil {
		fields = append(fields,
			ui.Field{Label: "SHA256", Value: strings.TrimPrefix(l.Fingerprint(), "SHA256:"), Tone: ui.ToneDim},
			ui.Field{Label: "Local", Value: l.Describe()},
		)
	}
	return fields
}
