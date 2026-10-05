package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/banner"
)

func writeBannerFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestParseBannerArgs(t *testing.T) {
	t.Run("publish requires a file", func(t *testing.T) {
		if _, err := parseBannerArgs(nil); err == nil {
			t.Error("expected an error for no arguments")
		}
	})

	t.Run("private and password are mutually exclusive", func(t *testing.T) {
		if _, err := parseBannerArgs([]string{"a.html", "--private", "--password", "x"}); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("site requires update-key and vice versa", func(t *testing.T) {
		if _, err := parseBannerArgs([]string{"a.html", "--site", "abc"}); err == nil {
			t.Error("expected an error for --site without --update-key")
		}
		if _, err := parseBannerArgs([]string{"a.html", "--update-key", "k"}); err == nil {
			t.Error("expected an error for --update-key without --site")
		}
	})

	t.Run("unpublish requires site and update-key, no file", func(t *testing.T) {
		if _, err := parseBannerArgs([]string{"--unpublish"}); err == nil {
			t.Error("expected an error for --unpublish with no --site/--update-key")
		}
		if _, err := parseBannerArgs([]string{"a.html", "--unpublish", "--site", "abc", "--update-key", "k"}); err == nil {
			t.Error("expected an error for --unpublish with a file")
		}
		if _, err := parseBannerArgs([]string{"--unpublish", "--site", "abc", "--update-key", "k", "--private"}); err == nil {
			t.Error("expected an error for --unpublish combined with --private")
		}
		opts, err := parseBannerArgs([]string{"--unpublish", "--site", "abc", "--update-key", "k"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !opts.unpublish || opts.site != "abc" || opts.updateKey != "k" {
			t.Errorf("unexpected parsed opts: %+v", opts)
		}
	})

	t.Run("republish parses file plus site and update-key", func(t *testing.T) {
		opts, err := parseBannerArgs([]string{"a.html", "--site", "abc", "--update-key", "k"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.file != "a.html" || opts.site != "abc" || opts.updateKey != "k" {
			t.Errorf("unexpected parsed opts: %+v", opts)
		}
	})

	t.Run("unknown flag is rejected", func(t *testing.T) {
		if _, err := parseBannerArgs([]string{"a.html", "--bogus"}); err == nil {
			t.Error("expected an error for an unknown flag")
		}
	})
}

func TestRunBannerPublish(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		json.NewEncoder(w).Encode(banner.Site{
			URL:       "https://plans.example.com/abc123",
			UpdateKey: "secret-key",
			SiteID:    "abc123",
			Status:    "published",
		})
	}))
	defer srv.Close()

	file := writeBannerFixture(t, `<html><body>hi</body></html>`)
	opts := bannerArgs{file: file}
	client := &banner.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	var stdout, stderr bytes.Buffer
	code := runBanner(opts, client, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "https://plans.example.com/abc123") {
		t.Errorf("expected the published URL in stdout, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "secret-key") {
		t.Errorf("expected the update_key in stdout, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "ONLY credential") {
		t.Errorf("expected the update_key warning in stdout, got: %s", stdout.String())
	}
	if gotBody["password"] != "" {
		t.Errorf("expected no password sent without --private/--password, got %q", gotBody["password"])
	}
}

func TestRunBannerPublishPrivateShowsPasswordOnce(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(banner.Site{URL: "https://plans.example.com/abc123", UpdateKey: "k", SiteID: "abc123"})
	}))
	defer srv.Close()

	file := writeBannerFixture(t, `<html></html>`)
	opts := bannerArgs{file: file, private: true}
	client := &banner.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	var stdout, stderr bytes.Buffer
	code := runBanner(opts, client, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, stderr: %s", stderr.String())
	}
	if gotBody["password"] == "" {
		t.Error("expected a generated password to be sent to the backend")
	}
	if !strings.Contains(stdout.String(), gotBody["password"]) {
		t.Errorf("expected the generated password to be printed, stdout: %s", stdout.String())
	}
}

func TestRunBannerPublishAmbiguousFailureWarnsAboutLostKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	file := writeBannerFixture(t, `<html></html>`)
	opts := bannerArgs{file: file}
	client := &banner.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	var stdout, stderr bytes.Buffer
	code := runBanner(opts, client, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected a non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "lost") {
		t.Errorf("expected the lost-key warning in stderr, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Do not retry") {
		t.Errorf("expected the do-not-retry warning in stderr, got: %s", stderr.String())
	}
}

func TestRunBannerRepublish(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(banner.Site{URL: "https://plans.example.com/abc123", SiteID: "abc123"})
	}))
	defer srv.Close()

	file := writeBannerFixture(t, `<html>new content</html>`)
	opts := bannerArgs{file: file, site: "abc123", updateKey: "secret-key"}
	client := &banner.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	var stdout, stderr bytes.Buffer
	code := runBanner(opts, client, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, stderr: %s", stderr.String())
	}
	if gotPath != "/v1/sites/abc123" {
		t.Errorf("expected a PUT to /v1/sites/abc123, got %q", gotPath)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("expected the update_key as bearer auth, got %q", gotAuth)
	}
	if !strings.Contains(stdout.String(), "republished") {
		t.Errorf("expected a republished confirmation, got: %s", stdout.String())
	}
}

func TestRunBannerUnpublish(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(banner.Site{SiteID: "abc123"})
	}))
	defer srv.Close()

	opts := bannerArgs{site: "abc123", updateKey: "secret-key", unpublish: true}
	client := &banner.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	var stdout, stderr bytes.Buffer
	code := runBanner(opts, client, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, stderr: %s", stderr.String())
	}
	if gotBody["html_content"] != banner.UnpublishPlaceholderHTML {
		t.Errorf("expected the placeholder HTML to be sent, got: %q", gotBody["html_content"])
	}
	if gotBody["password"] == "" {
		t.Error("expected a discard password to be sent")
	}
	if strings.Contains(stdout.String(), gotBody["password"]) {
		t.Error("the discarded unpublish password must never be printed")
	}
	if !strings.Contains(stdout.String(), "still live") {
		t.Errorf("expected the still-live caveat in stdout, got: %s", stdout.String())
	}
}

func TestRunBannerPublishWarnsWhenSiteIDRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(banner.Site{URL: "https://plans.example.com/x", UpdateKey: "lost-key", SiteID: "../bad"})
	}))
	defer srv.Close()

	file := writeBannerFixture(t, `<html></html>`)
	client := &banner.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	var stdout, stderr bytes.Buffer
	if code := runBanner(bannerArgs{file: file}, client, &stdout, &stderr); code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "lost-key") || !strings.Contains(out, "NEVER be republished or") {
		t.Errorf("expected the update_key and the NEVER warning, got: %s", out)
	}
	if strings.Contains(out, "site_id:") {
		t.Errorf("the rejected site_id must not be printed, got: %s", out)
	}
}

func TestRunBannerPublishWarnsOnUnpaintedPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(banner.Site{URL: "https://plans.example.com/a", UpdateKey: "k", SiteID: "a"})
	}))
	defer srv.Close()
	client := &banner.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}

	var stdout, stderr bytes.Buffer
	file := writeBannerFixture(t, `<html><body>hi</body></html>`)
	if code := runBanner(bannerArgs{file: file}, client, &stdout, &stderr); code != 0 {
		t.Fatalf("a self-paint warning must not block publishing, got exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "sets a background for the page itself") {
		t.Errorf("expected the self-paint warning, got: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	file = writeBannerFixture(t, `<html><body style="background:#000">hi</body></html>`)
	runBanner(bannerArgs{file: file}, client, &stdout, &stderr)
	if strings.Contains(stderr.String(), "sets a background for the page itself") {
		t.Errorf("painted page must not warn, got: %s", stderr.String())
	}
}

// B7
func TestParseBannerArgsRefusesRiskyFlagValues(t *testing.T) {
	cases := map[string][]string{
		"empty password":            {"a.html", "--password", ""},
		"blank password":            {"a.html", "--password", "  "},
		"password eats next flag":   {"a.html", "--password", "--site", "abc", "--update-key", "k"},
		"empty site":                {"a.html", "--site", "", "--update-key", "k"},
		"site eats next flag":       {"a.html", "--site", "--update-key", "k"},
		"empty update-key":          {"a.html", "--site", "abc", "--update-key", ""},
		"update-key eats next flag": {"a.html", "--site", "abc", "--update-key", "--private"},
		"empty = form":              {"a.html", "--password="},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseBannerArgs(args); err == nil {
				t.Errorf("expected an error for %v", args)
			}
		})
	}

	t.Run("empty password error names the public-page risk", func(t *testing.T) {
		_, err := parseBannerArgs([]string{"a.html", "--password", ""})
		if err == nil || !strings.Contains(err.Error(), "PUBLIC") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("= form allows a leading --", func(t *testing.T) {
		a, err := parseBannerArgs([]string{"a.html", "--password=--weird"})
		if err != nil || a.password != "--weird" {
			t.Errorf("got %+v, %v", a, err)
		}
	})

	t.Run("normal values still work", func(t *testing.T) {
		a, err := parseBannerArgs([]string{"a.html", "--site", "abc", "--update-key", "-k-"})
		if err != nil || a.site != "abc" || a.updateKey != "-k-" {
			t.Errorf("got %+v, %v", a, err)
		}
	})
}

// B8
func TestRunBannerRepublishNotesUntouchedPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(banner.Site{URL: "https://plans.example.com/abc123"})
	}))
	defer srv.Close()
	client := &banner.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	file := writeBannerFixture(t, `<html><body style="background:#fff">hi</body></html>`)

	var stdout, stderr bytes.Buffer
	if code := runBanner(bannerArgs{file: file, site: "abc123", updateKey: "k"}, client, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "did not touch the page's password") || !strings.Contains(stdout.String(), "stores nothing") {
		t.Errorf("expected the untouched-password note, got: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runBanner(bannerArgs{file: file, site: "abc123", updateKey: "k", password: "pw"}, client, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "did not touch") {
		t.Errorf("note must not appear when a password was set, got: %s", stdout.String())
	}
}
