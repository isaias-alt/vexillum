package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubSource(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"tag_name":"v1.0.0","draft":false,"prerelease":false},{"tag_name":"v1.1.0-canary.1","draft":false,"prerelease":true}]`))
	})
	mux.HandleFunc("/o/r/releases/download/v1.0.0/vexillum_1.0.0_linux_amd64.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("archive"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	g := &GitHubSource{Owner: "o", Repo: "r", APIBase: srv.URL, DownloadBase: srv.URL}
	releases, err := g.Releases(context.Background())
	if err != nil || len(releases) != 2 || releases[1].Tag != "v1.1.0-canary.1" || !releases[1].Prerelease {
		t.Fatalf("releases = %+v, %v", releases, err)
	}
	data, err := g.Asset(context.Background(), "v1.0.0", "vexillum_1.0.0_linux_amd64.tar.gz")
	if err != nil || string(data) != "archive" {
		t.Fatalf("asset = %q, %v", data, err)
	}
	if _, err := g.Asset(context.Background(), "v1.0.0", "missing.tar.gz"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing asset err = %v", err)
	}
}
