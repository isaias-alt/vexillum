// Package banner is vexillum's own client for forum-tool's "share"
// contract (docs/self-hosting-share.md in upstream): a
// single POST to publish an HTML page and a single PUT to republish it
// in place, against a remote host - by default ht-ml.app, a third-party
// service, not something vexillum operates. This is deliberately a thin
// HTTP client, not a hosting backend: see the scout report referenced from
// the mission that added this package for why running vexillum's own
// public-facing host would be a different category of project entirely.
package banner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// DefaultBaseURL is ht-ml.app's API - the default target for a Client
// with no BaseURL of its own.
const DefaultBaseURL = "https://api.ht-ml.app"

// requestTimeout bounds both the create and the republish request, per
// forum-tool's own contract. Past it, a request that already reached the
// server can't be told apart from one that never did.
const requestTimeout = 30 * time.Second

// Client talks to a share backend implementing forum-tool's
// self-hosting-share.md contract (POST /v1/sites, PUT /v1/sites/{id}).
// The zero value is not ready to use - call NewClient.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient returns a Client targeting DefaultBaseURL with a
// requestTimeout-bound http.Client.
func NewClient() *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Timeout: requestTimeout},
	}
}

// Site is a published (or republished) page's identity and credentials,
// as returned by the backend.
type Site struct {
	URL       string `json:"url"`
	UpdateKey string `json:"update_key"`
	SiteID    string `json:"site_id"`
	Status    string `json:"status"`

	// SiteIDRejected is set by Create when the host returned a site_id that
	// failed validateSiteID. The id is then cleared (treated as absent) so it
	// never reaches a URL path, but url and update_key are still returned: the
	// page is live, and the caller must tell the user it can never be
	// republished or unpublished, since --site is half the republish credential.
	SiteIDRejected bool `json:"-"`
}

// AmbiguousCreateError wraps a failure from Create where it can't be told
// whether the POST reached the server and actually published the page.
// This matters because POST /v1/sites is not idempotent - every attempt
// creates a brand-new page - and because update_key is returned exactly
// once: if the request in fact succeeded server-side, that update_key was
// in the response this process never received (or couldn't parse), and it
// is unrecoverable. A caller must never retry a Create on this error
// assuming it's safe to do so.
type AmbiguousCreateError struct {
	Cause error
}

func (e *AmbiguousCreateError) Error() string {
	return fmt.Sprintf("could not confirm the page was created: %v - if it was, its update_key was lost with this response and cannot be recovered by any means; do not retry assuming this is safe, a retry would create a separate, new page", e.Cause)
}

func (e *AmbiguousCreateError) Unwrap() error { return e.Cause }

// siteIDPattern is the charset share accepts for a backend-returned
// site_id, per forum-tool's contract: it's interpolated into the PUT path
// unescaped, so an id outside this set (or made entirely of dots, which
// could resolve to "." or "..") is refused rather than trusted.
var siteIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validateSiteID(id string) error {
	if id == "" {
		return nil // optional per contract - a creation-only backend
	}
	if !siteIDPattern.MatchString(id) {
		return fmt.Errorf("site_id %q contains characters outside A-Za-z0-9._- - refusing to trust it in a URL path", id)
	}
	if allDots(id) {
		return fmt.Errorf("site_id %q is made entirely of dots - refusing to trust it in a URL path", id)
	}
	return nil
}

func allDots(s string) bool {
	for _, r := range s {
		if r != '.' {
			return false
		}
	}
	return true
}

type siteRequest struct {
	HTMLContent string `json:"html_content"`
	Password    string `json:"password,omitempty"`
}

// Create publishes html as a brand-new page, optionally gated by
// password. Per contract, a response missing either url or update_key is
// always treated as an error, wrapped as an AmbiguousCreateError - a
// response that parses cleanly but omits one of those two fields is just
// as unrecoverable as a network failure would have been. A site_id that
// fails validation is not an error: see Site.SiteIDRejected.
func (c *Client) Create(html, password string) (Site, error) {
	site, _, err := c.request(http.MethodPost, "/v1/sites", "", siteRequest{HTMLContent: html, Password: password})
	if err != nil {
		return Site{}, &AmbiguousCreateError{Cause: err}
	}
	if site.URL == "" || site.UpdateKey == "" {
		return Site{}, &AmbiguousCreateError{Cause: fmt.Errorf("response is missing url or update_key")}
	}
	// An invalid echoed site_id is untrusted input: treat it as absent rather
	// than discarding the whole result, which would throw away the update_key
	// of a page that is already live.
	if err := validateSiteID(site.SiteID); err != nil {
		site.SiteID = ""
		site.SiteIDRejected = true
	}
	return site, nil
}

// Update republishes an existing page's content (and, when password is
// non-empty, sets or rotates its password) in place, authorizing with
// updateKey - the page's only write credential. Unlike Create, PUT
// replaces the same page rather than making a new one, so a failure here
// is always safe to retry with the same arguments.
func (c *Client) Update(siteID, updateKey, html, password string) (Site, error) {
	if err := validateSiteID(siteID); err != nil {
		return Site{}, err
	}
	site, status, err := c.request(http.MethodPut, "/v1/sites/"+siteID, updateKey, siteRequest{HTMLContent: html, Password: password})
	if err != nil {
		if status == http.StatusUnauthorized {
			return Site{}, fmt.Errorf("rejected: this update_key does not match site %q (401) - nothing was changed", siteID)
		}
		return Site{}, fmt.Errorf("republish did not complete: %w (safe to retry - a PUT replaces the same page, it never creates a new one)", err)
	}
	return site, nil
}

// request performs a single JSON request against the backend and decodes
// a Site from a 2xx response. status is returned even on error, so a
// caller can distinguish a clear rejection (e.g. 401) from an ambiguous
// one.
func (c *Client) request(method, path, bearer string, body siteRequest) (Site, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return Site{}, 0, fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequest(method, c.baseURL()+path, bytes.NewReader(payload))
	if err != nil {
		return Site{}, 0, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Site{}, 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Site{}, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Site{}, resp.StatusCode, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, trimBody(raw))
	}

	var site Site
	if err := json.Unmarshal(raw, &site); err != nil {
		return Site{}, resp.StatusCode, fmt.Errorf("parsing response: %w", err)
	}
	return site, resp.StatusCode, nil
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: requestTimeout}
}

func trimBody(raw []byte) string {
	const max = 500
	s := strings.TrimSpace(string(raw))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
