package banner

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
}

func TestCreateSuccess(t *testing.T) {
	var gotBody siteRequest
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sites" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		json.NewEncoder(w).Encode(Site{
			URL:       "https://plans.example.com/abc123",
			UpdateKey: "secret-key",
			SiteID:    "abc123",
			Status:    "published",
		})
	})

	site, err := c.Create("<html></html>", "hunter2")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if site.URL != "https://plans.example.com/abc123" || site.UpdateKey != "secret-key" || site.SiteID != "abc123" {
		t.Errorf("unexpected site: %+v", site)
	}
	if gotBody.HTMLContent != "<html></html>" || gotBody.Password != "hunter2" {
		t.Errorf("unexpected request body: %+v", gotBody)
	}
}

func TestCreateMissingURLOrUpdateKeyIsAmbiguous(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Site{SiteID: "abc123", Status: "published"})
	})

	_, err := c.Create("<html></html>", "")
	if err == nil {
		t.Fatal("expected an error when the response omits url and update_key")
	}
	var ambiguous *AmbiguousCreateError
	if !errors.As(err, &ambiguous) {
		t.Errorf("expected an AmbiguousCreateError, got %T: %v", err, err)
	}
}

func TestCreateNetworkFailureIsAmbiguous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server doesn't support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		conn.Close() // simulate an ambiguous mid-request failure
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := c.Create("<html></html>", "")
	if err == nil {
		t.Fatal("expected an error for a dropped connection")
	}
	var ambiguous *AmbiguousCreateError
	if !errors.As(err, &ambiguous) {
		t.Errorf("expected an AmbiguousCreateError, got %T: %v", err, err)
	}
}

func TestCreateTreatsUntrustworthySiteIDAsAbsentButKeepsUpdateKey(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Site{
			URL:       "https://plans.example.com/abc123",
			UpdateKey: "secret-key",
			SiteID:    "../etc/passwd",
		})
	})

	site, err := c.Create("<html></html>", "")
	if err != nil {
		t.Fatalf("Create must not fail for a live page with a bad site_id: %v", err)
	}
	if site.URL == "" || site.UpdateKey != "secret-key" {
		t.Errorf("url and update_key must survive, got %+v", site)
	}
	if site.SiteID != "" || !site.SiteIDRejected {
		t.Errorf("expected the bad site_id cleared and flagged, got %+v", site)
	}
}

func TestUpdateSuccess(t *testing.T) {
	var gotAuth string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/sites/abc123" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(Site{URL: "https://plans.example.com/abc123", SiteID: "abc123", Status: "published"})
	})

	site, err := c.Update("abc123", "secret-key", "<html>new</html>", "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if site.URL != "https://plans.example.com/abc123" {
		t.Errorf("unexpected site: %+v", site)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("expected Authorization to carry the update_key, got %q", gotAuth)
	}
}

func TestUpdateOmittedURLIsNotAnError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Site{SiteID: "abc123", Status: "published"})
	})

	site, err := c.Update("abc123", "secret-key", "<html>new</html>", "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if site.URL != "" {
		t.Errorf("expected no URL, got %q", site.URL)
	}
}

func TestUpdateRejectedKeyReturns401Message(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := c.Update("abc123", "wrong-key", "<html></html>", "")
	if err == nil {
		t.Fatal("expected an error for a rejected update_key")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("expected a clear 401 rejection message, got: %v", err)
	}
}

func TestUpdateRejectsUntrustworthySiteIDBeforeSendingRequest(t *testing.T) {
	called := false
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	_, err := c.Update("../etc", "secret-key", "<html></html>", "")
	if err == nil {
		t.Fatal("expected an error for an untrustworthy site id")
	}
	if called {
		t.Error("expected no request to be sent for an untrustworthy site id")
	}
}
