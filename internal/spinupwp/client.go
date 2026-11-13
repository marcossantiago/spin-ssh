// Package spinupwp is a small client for the parts of the SpinupWP API spin-ssh needs.
// API docs: https://api.spinupwp.com/
package spinupwp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the SpinupWP API base URL.
const DefaultBaseURL = "https://api.spinupwp.app/v1"

// Client talks to the SpinupWP API with a personal or team API token.
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
	maxRetries int
}

// New returns a client for token.
func New(token string) *Client {
	return &Client{
		token:      token,
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		maxRetries: 3,
	}
}

// Server is a SpinupWP server. Only the fields spin-ssh uses are decoded.
type Server struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
	SSHPort   int    `json:"ssh_port"`
}

// SSHKeyRef is a team SSH key granted to a site user.
type SSHKeyRef struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

// Site is a SpinupWP site. Only the fields spin-ssh uses are decoded.
type Site struct {
	ID         int64       `json:"id"`
	ServerID   int64       `json:"server_id"`
	Domain     string      `json:"domain"`
	SiteUser   string      `json:"site_user"`
	UserAuth   string      `json:"user_auth"`
	PHPVersion string      `json:"php_version"`
	Status     string      `json:"status"`
	SSHKeys    []SSHKeyRef `json:"ssh_keys"`
}

// SSHKey is a public key on the SpinupWP team.
type SSHKey struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publickey"`
}

// Event is an asynchronous operation, such as adding a key to a site user.
type Event struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Output *string `json:"output"`
}

// Error is a non-2xx response from the API.
type Error struct {
	StatusCode int
	Message    string              `json:"message"`
	Errors     map[string][]string `json:"errors"`
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("spinupwp: %d %s", e.StatusCode, http.StatusText(e.StatusCode))
	if e.Message != "" {
		msg += ": " + e.Message
	}
	for field, errs := range e.Errors {
		msg += fmt.Sprintf("; %s: %s", field, strings.Join(errs, ", "))
	}
	return msg
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// ListServers returns all servers.
func (c *Client) ListServers(ctx context.Context) ([]Server, error) {
	return list[Server](ctx, c, "/servers")
}

// ListSites returns all sites.
func (c *Client) ListSites(ctx context.Context) ([]Site, error) {
	return list[Site](ctx, c, "/sites")
}

// ListSSHKeys returns the team's SSH keys.
func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	return list[SSHKey](ctx, c, "/ssh-keys")
}

// AddSiteUserKey grants a team SSH key to a site's user. It returns the event to wait
// for, or 0 when the site user already had the key.
func (c *Client) AddSiteUserKey(ctx context.Context, siteID, keyID int64) (int64, error) {
	var out struct {
		EventID *int64 `json:"event_id"`
	}
	form := url.Values{"ssh_key_id": {strconv.FormatInt(keyID, 10)}}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/sites/%d/site-user/ssh-keys", siteID), form, &out); err != nil {
		return 0, err
	}
	if out.EventID == nil {
		return 0, nil
	}
	return *out.EventID, nil
}

// RemoveSiteUserKey takes a team SSH key away from a site's user. It returns the event
// to wait for, or 0 when the site user didn't have the key.
func (c *Client) RemoveSiteUserKey(ctx context.Context, siteID, keyID int64) (int64, error) {
	var out struct {
		EventID *int64 `json:"event_id"`
	}
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/sites/%d/site-user/ssh-keys/%d", siteID, keyID), nil, &out); err != nil {
		return 0, err
	}
	if out.EventID == nil {
		return 0, nil
	}
	return *out.EventID, nil
}

// WaitForEvent polls an event until it is deployed, fails, or ctx is done.
// An event ID of 0 means there is nothing to wait for.
func (c *Client) WaitForEvent(ctx context.Context, id int64) error {
	if id == 0 {
		return nil
	}
	for {
		var out struct {
			Data Event `json:"data"`
		}
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/events/%d", id), nil, &out); err != nil {
			return err
		}
		switch strings.ToLower(out.Data.Status) {
		case "deployed":
			return nil
		case "failed":
			msg := fmt.Sprintf("spinupwp: event %d (%s) failed", out.Data.ID, out.Data.Name)
			if out.Data.Output != nil && strings.TrimSpace(*out.Data.Output) != "" {
				msg += ":\n" + strings.TrimSpace(*out.Data.Output)
			}
			return errors.New(msg)
		}
		if err := sleep(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}

// list fetches every page of a list endpoint.
func list[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		var out struct {
			Data       []T `json:"data"`
			Pagination struct {
				Next *string `json:"next"`
			} `json:"pagination"`
		}
		q := url.Values{"limit": {"100"}, "page": {strconv.Itoa(page)}}
		if err := c.do(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Data...)
		if out.Pagination.Next == nil || *out.Pagination.Next == "" || len(out.Data) == 0 {
			return all, nil
		}
	}
}

// do sends a request (form-encoded when body is set) and decodes the JSON response into
// out. It retries 429s, honouring Retry-After: the API allows 60 requests a minute.
func (c *Client) do(ctx context.Context, method, path string, body url.Values, out any) error {
	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = strings.NewReader(body.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
		if err != nil {
			return fmt.Errorf("spinupwp: building request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "spin-ssh")
		if body != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("spinupwp: %s %s: %w", method, path, err)
		}
		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("spinupwp: reading response: %w", err)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out == nil || len(respBody) == 0 {
				return nil
			}
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("spinupwp: decoding response: %w", err)
			}
			return nil
		}

		apiErr := &Error{StatusCode: resp.StatusCode}
		_ = json.Unmarshal(respBody, apiErr)
		if resp.StatusCode != http.StatusTooManyRequests || attempt >= c.maxRetries {
			return apiErr
		}
		wait := 10 * time.Second
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs >= 0 {
			wait = time.Duration(secs) * time.Second
		}
		if err := sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
