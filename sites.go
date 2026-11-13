package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marcossantiago/spin-ssh/internal/spinupwp"
	"github.com/marcossantiago/spin-ssh/internal/store"
	"github.com/marcossantiago/spin-ssh/internal/ui"
)

// staleAfter is when the picker refreshes the cache in the background. The cached list
// shows immediately either way.
const staleAfter = 15 * time.Minute

// pickSite shows the sites (only those keep returns true for, when set) and returns the
// chosen one, or nil if the user quit. A query matching exactly one site skips the picker.
func pickSite(client *spinupwp.Client, cfg *store.Config, opts options, title string, keep func(store.Site) bool) (*store.Site, error) {
	inv, err := store.LoadInventory()
	if err != nil {
		return nil, err
	}
	fresh := false
	if inv == nil || opts.refresh {
		err := ui.Run("Loading servers and sites from SpinupWP…", func(ctx context.Context) error {
			inv, err = fetchInventory(ctx, client)
			return err
		})
		if err != nil {
			return nil, err
		}
		fresh = true
	}

	recent := store.LoadRecent()
	toItems := func(inv *store.Inventory) []ui.Item {
		sites := slices.Clone(inv.Sites)
		// Recently used sites first, most recent at the top; ties in search keep this order.
		slices.SortStableFunc(sites, func(a, b store.Site) int { return recent[b.ID].Compare(recent[a.ID]) })
		var items []ui.Item
		for _, s := range sites {
			if keep == nil || keep(s) {
				items = append(items, siteItem(s, cfg, recent[s.ID]))
			}
		}
		return items
	}
	items := toItems(inv)
	if len(items) == 0 {
		if keep != nil {
			return nil, fmt.Errorf("your key %q isn't on any site user", cfg.SSHKeyName)
		}
		return nil, fmt.Errorf("no sites found in SpinupWP")
	}

	if opts.query != "" {
		if m := ui.Filter(items, opts.query); len(m) == 1 {
			s := items[m[0]].Value.(store.Site)
			return &s, nil
		}
	}

	action := "connect"
	if keep != nil {
		action = "revoke"
	}
	it, err := ui.Pick(ui.PickerOptions{
		Title:       title,
		Placeholder: "Search by domain or server",
		Noun:        "site",
		Headers:     []string{"Domain", "Server"},
		BadgeHeader: "Last used",
		Action:      action,
		Query:       opts.query,
		Items:       items,
		Status:      age(inv.FetchedAt),
		AutoRefresh: !fresh && time.Since(inv.FetchedAt) > staleAfter,
		Refresh: func() ui.Refreshed {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			inv, err := fetchInventory(ctx, client)
			if err != nil {
				return ui.Refreshed{Err: err}
			}
			return ui.Refreshed{Items: toItems(inv), Status: age(inv.FetchedAt)}
		},
	})
	if err != nil || it == nil {
		return nil, err
	}
	s := it.Value.(store.Site)
	return &s, nil
}

// fetchInventory loads servers and sites, joins them and saves the cache.
func fetchInventory(ctx context.Context, client *spinupwp.Client) (*store.Inventory, error) {
	servers, err := client.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	sites, err := client.ListSites(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]spinupwp.Server{}
	for _, s := range servers {
		byID[s.ID] = s
	}

	inv := &store.Inventory{FetchedAt: time.Now()}
	for _, s := range sites {
		srv := byID[s.ServerID]
		keys := make([]store.Key, len(s.SSHKeys))
		for i, k := range s.SSHKeys {
			keys[i] = store.Key{ID: k.ID, Name: k.Name}
		}
		inv.Sites = append(inv.Sites, store.Site{
			ID:         s.ID,
			Domain:     s.Domain,
			SiteUser:   s.SiteUser,
			UserAuth:   s.UserAuth,
			PHPVersion: s.PHPVersion,
			ServerID:   s.ServerID,
			ServerName: srv.Name,
			Host:       srv.IPAddress,
			Port:       srv.SSHPort,
			Keys:       keys,
		})
	}
	slices.SortFunc(inv.Sites, func(a, b store.Site) int { return strings.Compare(a.Domain, b.Domain) })
	return inv, store.SaveInventory(inv)
}

func siteItem(s store.Site, cfg *store.Config, lastUsed time.Time) ui.Item {
	access := ui.Field{Label: "Access", Value: "Your key is on this site user", Tone: ui.ToneGood}
	switch {
	case s.UserAuth == "password":
		access = ui.Field{Label: "Access", Value: "Password login only: SpinupWP can't add keys", Tone: ui.ToneBad}
	case !s.HasKey(cfg.SSHKeyID):
		access = ui.Field{Label: "Access", Value: "Your key is added when you connect", Tone: ui.ToneWarn}
	}
	names := make([]string, len(s.Keys))
	for i, k := range s.Keys {
		names[i] = k.Name
	}
	address := s.Host
	if s.Port != 0 && s.Port != 22 {
		address += ":" + strconv.Itoa(s.Port)
	}
	badge, used := "", "Never"
	if !lastUsed.IsZero() {
		badge = ago(lastUsed)
		used = badge
	}

	fields := []ui.Field{
		{Label: "Server", Value: s.ServerName},
		{Label: "Address", Value: address},
		{Label: "User", Value: s.SiteUser},
		{Label: "PHP", Value: s.PHPVersion},
		access,
		{Label: "Keys", Value: cmp.Or(strings.Join(names, ", "), "None"), Tone: ui.ToneDim},
		{Label: "Last used", Value: used, Tone: ui.ToneDim},
	}
	return ui.Item{
		Key:     strconv.FormatInt(s.ID, 10),
		Columns: []string{s.Domain, s.ServerName},
		Badge:   badge,
		Title:   s.Domain,
		Fields:  fields,
		Value:   s,
	}
}

// age describes how old the cache is, for the picker's status.
func age(t time.Time) string {
	if time.Since(t) < time.Minute {
		return "updated just now"
	}
	return "updated " + ago(t)
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
