package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Release is one GitHub release, reduced to what channel selection needs.
type Release struct {
	// Tag is the git tag, "v0.4.0" or "v0.4.0-canary.2".
	Tag        string
	Draft      bool
	Prerelease bool
}

// Version is the tag without its leading "v": what the binary reports and
// what the archive names carry.
func (r Release) Version() string { return trimV(r.Tag) }

// Source is where releases and their assets come from. The real one is
// GitHubSource; tests use fakes so nothing needs the network.
type Source interface {
	// Releases lists the published releases, in no particular order.
	Releases(ctx context.Context) ([]Release, error)
	// Asset downloads one file attached to the release with the given tag.
	Asset(ctx context.Context, tag, name string) ([]byte, error)
}

// maxAssetBytes bounds a download: a release archive is a few megabytes, so
// anything near this is not one.
const maxAssetBytes = 200 << 20

// GitHubSource reads GitHub Releases over HTTP.
type GitHubSource struct {
	Owner, Repo string
	// Client defaults to http.DefaultClient; per-request deadlines come from
	// the context.
	Client *http.Client
	// APIBase and DownloadBase default to GitHub's; tests point them at a
	// local server.
	APIBase      string
	DownloadBase string
}

// The GitHub endpoints. Plain string variables, not constants, so a test
// build can point a whole binary at a local server with
// -ldflags "-X <this package>.apiBase=..." for an end-to-end run.
var (
	apiBase      = "https://api.github.com"
	downloadBase = "https://github.com"
)

// NewGitHubSource returns the source for owner/repo on github.com.
func NewGitHubSource(owner, repo string) *GitHubSource {
	return &GitHubSource{
		Owner:        owner,
		Repo:         repo,
		Client:       &http.Client{Timeout: 5 * time.Minute},
		APIBase:      apiBase,
		DownloadBase: downloadBase,
	}
}

func (g *GitHubSource) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

func (g *GitHubSource) get(ctx context.Context, rawURL, accept string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rawURL, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", rawURL, limit)
	}
	return data, nil
}

// Releases implements Source. It reads the first page of 100 releases, which
// holds the newest ones; older releases are never candidates.
func (g *GitHubSource) Releases(ctx context.Context) ([]Release, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=100", g.APIBase, url.PathEscape(g.Owner), url.PathEscape(g.Repo))
	body, err := g.get(ctx, u, "application/vnd.github+json", 8<<20)
	if err != nil {
		return nil, fmt.Errorf("listing releases: %w", err)
	}
	var raw []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decoding the release list: %w", err)
	}
	out := make([]Release, 0, len(raw))
	for _, r := range raw {
		out = append(out, Release{Tag: r.Tag, Draft: r.Draft, Prerelease: r.Prerelease})
	}
	return out, nil
}

// Asset implements Source.
func (g *GitHubSource) Asset(ctx context.Context, tag, name string) ([]byte, error) {
	u := fmt.Sprintf("%s/%s/%s/releases/download/%s/%s", g.DownloadBase, url.PathEscape(g.Owner), url.PathEscape(g.Repo), url.PathEscape(tag), url.PathEscape(name))
	data, err := g.get(ctx, u, "", maxAssetBytes)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", name, err)
	}
	return data, nil
}

func trimV(tag string) string {
	if len(tag) > 0 && tag[0] == 'v' {
		return tag[1:]
	}
	return tag
}
