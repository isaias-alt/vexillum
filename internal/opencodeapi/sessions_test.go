package opencodeapi

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// The Sessions side of the package, against the same kind of stub as the
// wake path's tests.

func TestEnsureService_LeavesARunningServiceAlone(t *testing.T) {
	bin, log := stub(t, running)
	if err := (CLI{Binary: bin}).EnsureService(); err != nil {
		t.Fatal(err)
	}
	for _, c := range calls(t, log) {
		if strings.HasPrefix(c, "service start") {
			t.Errorf("a running service was started again: %q", c)
		}
	}
}

func TestEnsureService_StartsAStoppedServiceExplicitly(t *testing.T) {
	dir := t.TempDir()
	marker := dir + "/started"
	bin, log := stub(t, `
if [ "$1 $2" = "service status" ]; then if [ -f '`+marker+`' ]; then echo http://127.0.0.1:49778; else echo stopped; fi; exit 0; fi
if [ "$1 $2" = "service start" ]; then : > '`+marker+`'; echo http://127.0.0.1:49778; exit 0; fi
echo "unexpected: $*" >&2; exit 9`)
	if err := (CLI{Binary: bin}).EnsureService(); err != nil {
		t.Fatal(err)
	}
	got := calls(t, log)
	want := []string{"service status", "service start", "service status"}
	if !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q: the service is started by 'service start', never by 'api' auto-starting it", got, want)
	}
}

func TestEnsureService_FailuresAreNamed(t *testing.T) {
	bin, _ := stub(t, `
if [ "$1 $2" = "service status" ]; then echo stopped; exit 0; fi
if [ "$1 $2" = "service start" ]; then echo 'port in use' >&2; exit 2; fi`)
	if err := (CLI{Binary: bin}).EnsureService(); err == nil || !strings.Contains(err.Error(), "port in use") {
		t.Errorf("EnsureService = %v", err)
	}
	// start returns 0 but nothing is running afterwards.
	bin, _ = stub(t, `
if [ "$1 $2" = "service status" ]; then echo stopped; exit 0; fi
exit 0`)
	if err := (CLI{Binary: bin}).EnsureService(); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("EnsureService = %v", err)
	}
}

// Two dispatches can race to start the service: the one that loses sees the
// start fail, finds the service running, and carries on.
func TestEnsureService_ALostStartRaceIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	bin, _ := stub(t, `
if [ "$1 $2" = "service status" ]; then
  n=0; [ -f '`+dir+`/n' ] && read -r n < '`+dir+`/n'; n=$((n+1)); echo $n > '`+dir+`/n'
  if [ $n -ge 2 ]; then echo http://127.0.0.1:49778; else echo stopped; fi
  exit 0
fi
if [ "$1 $2" = "service start" ]; then echo 'address already in use' >&2; exit 2; fi`)
	if err := (CLI{Binary: bin}).EnsureService(); err != nil {
		t.Fatalf("EnsureService = %v", err)
	}
}

// Every call, the service start included, runs with the config-layer
// variables removed, and keeps the rest of the environment.
func TestCalls_RunWithoutTheConfigLayerEnvironment(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG", "/poison/file.json")
	t.Setenv("OPENCODE_CONFIG_DIR", "/poison/dir")
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"poison/x"}`)
	t.Setenv("OPENCODE_DISABLE_AUTOUPDATE", "1")
	t.Setenv("VX_KEEP", "kept")
	dir := t.TempDir()
	envLog := dir + "/env"
	bin, _ := stub(t, `
printf '%s|%s|%s|%s|%s\n' "$1 $2" "$OPENCODE_CONFIG" "$OPENCODE_CONFIG_DIR" "$OPENCODE_CONFIG_CONTENT" "$OPENCODE_DISABLE_AUTOUPDATE-$VX_KEEP" >> '`+envLog+`'
if [ "$1 $2" = "service status" ]; then if [ -f '`+dir+`/up' ]; then echo http://127.0.0.1:1; else echo stopped; fi; exit 0; fi
if [ "$1 $2" = "service start" ]; then : > '`+dir+`/up'; exit 0; fi
echo '{"data":[]}'`)
	c := CLI{Binary: bin}
	if err := c.EnsureService(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Models("/camp"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(envLog)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 4 {
		t.Fatalf("env log = %q", lines)
	}
	for _, l := range lines {
		parts := strings.SplitN(l, "|", 5)
		if parts[1] != "" || parts[2] != "" || parts[3] != "" {
			t.Errorf("a config-layer variable reached opencode (%s): %q", parts[0], l)
		}
		if parts[4] != "1-kept" {
			t.Errorf("the rest of the environment was not kept (%s): %q", parts[0], l)
		}
	}
	if !strings.HasPrefix(lines[1], "service start|") {
		t.Errorf("second call = %q, want the service start", lines[1])
	}
}

func TestModels_ListsWhatTheServiceCanRunInTheDirectory(t *testing.T) {
	bin, log := stub(t, running+`
echo '{"location":{"directory":"/camp"},"data":[
 {"id":"m1","modelID":"m1","providerID":"outprov","variants":[{"id":"high","settings":{}},{"id":"low","settings":{}}],"enabled":true},
 {"id":"m2","providerID":"outprov","variants":[]},
 {"id":"off","providerID":"outprov","enabled":false},
 {"modelID":"only-model-id","providerID":"p"},
 {"id":"no-provider"}
]}'`)
	models, err := CLI{Binary: bin}.Models("/camp dir")
	if err != nil {
		t.Fatal(err)
	}
	want := []Model{
		{ProviderID: "outprov", ID: "m1", Variants: []string{"high", "low"}},
		{ProviderID: "outprov", ID: "m2"},
		{ProviderID: "p", ID: "only-model-id"},
	}
	if len(models) != len(want) {
		t.Fatalf("models = %+v", models)
	}
	for i := range want {
		if models[i].ProviderID != want[i].ProviderID || models[i].ID != want[i].ID || !slices.Equal(models[i].Variants, want[i].Variants) {
			t.Errorf("model %d = %+v, want %+v", i, models[i], want[i])
		}
	}
	got := calls(t, log)
	if len(got) != 2 || got[1] != "api GET /api/model -H x-opencode-directory: /camp dir" {
		t.Errorf("calls = %q: the project is named by the x-opencode-directory header", got)
	}
}

func TestModels_NeverStartsAService(t *testing.T) {
	bin, log := stub(t, `echo stopped`)
	if _, err := (CLI{Binary: bin}).Models("/camp"); !errors.Is(err, ErrServiceStopped) {
		t.Fatalf("Models = %v, want ErrServiceStopped", err)
	}
	for _, c := range calls(t, log) {
		if strings.HasPrefix(c, "api ") || strings.HasPrefix(c, "service start") {
			t.Errorf("a stopped service was started: %q", c)
		}
	}
}

func TestCreateSession_PostsTheModelAndTheLocationAndNothingElse(t *testing.T) {
	bin, log := stub(t, running+`
echo '{"data":{"id":"ses_eaa2d9d6cffe3U0WWSA8fu7NsB","projectID":"global"}}'`)
	id, err := CLI{Binary: bin}.CreateSession("/camp", "vexillum t1", SessionModel{ProviderID: "outprov", ID: "m1", Variant: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "ses_eaa2d9d6cffe3U0WWSA8fu7NsB" {
		t.Errorf("id = %q", id)
	}
	got := calls(t, log)
	if len(got) != 2 || !strings.HasPrefix(got[1], "api POST /api/session -d ") {
		t.Fatalf("calls = %q", got)
	}
	var sent map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[1], "api POST /api/session -d ")), &sent); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(sent))
	for k := range sent {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"location", "model", "title"}) {
		t.Errorf("body keys = %v: no agent and no permissions may be sent", keys)
	}
	if string(sent["model"]) != `{"providerID":"outprov","id":"m1","variant":"high"}` {
		t.Errorf("model = %s", sent["model"])
	}
	if string(sent["location"]) != `{"directory":"/camp"}` || string(sent["title"]) != `"vexillum t1"` {
		t.Errorf("location = %s title = %s", sent["location"], sent["title"])
	}
}

func TestCreateSession_NoVariantSendsNone(t *testing.T) {
	bin, log := stub(t, running+`
echo '{"data":{"id":"ses_abc"}}'`)
	if _, err := (CLI{Binary: bin}).CreateSession("/camp", "t", SessionModel{ProviderID: "p", ID: "m"}); err != nil {
		t.Fatal(err)
	}
	if got := calls(t, log); !strings.Contains(got[1], `"model":{"providerID":"p","id":"m"}`) {
		t.Errorf("the variant key should be absent: %q", got[1])
	}
}

func TestCreateSession_RefusesAnAnswerWithoutAUsableId(t *testing.T) {
	for _, answer := range []string{`{"data":{}}`, `{"data":{"id":"nope"}}`, `{"data":{"id":"ses_a/../b"}}`, `not json`} {
		bin, _ := stub(t, running+"\necho '"+answer+"'")
		if id, err := (CLI{Binary: bin}).CreateSession("/camp", "t", SessionModel{ProviderID: "p", ID: "m"}); err == nil {
			t.Errorf("answer %s gave session %q", answer, id)
		}
	}
}

func TestSession_ReadsTheLocationAndTheModel(t *testing.T) {
	bin, log := stub(t, running+`
echo '{"data":{"id":"ses_abc","model":{"id":"m1","providerID":"outprov","variant":"high"},"location":{"directory":"/camp"}}}'`)
	s, err := CLI{Binary: bin}.Session("ses_abc")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "ses_abc" || s.Directory != "/camp" || s.Model != (SessionModel{ProviderID: "outprov", ID: "m1", Variant: "high"}) {
		t.Errorf("session = %+v", s)
	}
	if got := calls(t, log); got[1] != "api GET /api/session/ses_abc" {
		t.Errorf("calls = %q", got)
	}
}

func TestSession_AMissingSessionIsRecognized(t *testing.T) {
	bin, _ := stub(t, running+`
echo '{"_tag":"SessionNotFoundError","message":"Session not found: ses_gone"}'
echo 'HTTP 404 Not Found' >&2
exit 1`)
	_, err := CLI{Binary: bin}.Session("ses_gone")
	if !IsSessionNotFound(err) {
		t.Fatalf("Session = %v, want a not-found error", err)
	}
	if _, err := (CLI{Binary: bin}).Session("../x"); err == nil {
		t.Error("an id that is not a session id was accepted")
	}
}

func TestCheckModel(t *testing.T) {
	models := []Model{
		{ProviderID: "outprov", ID: "m1", Variants: []string{"low", "high"}},
		{ProviderID: "outprov", ID: "m2"},
		{ProviderID: "other", ID: "x/with/slashes", Variants: []string{"max"}},
	}
	cases := []struct {
		name                     string
		models                   []Model
		provider, model, variant string
		wantErr                  []string
	}{
		{name: "model without variant", models: models, provider: "outprov", model: "m1"},
		{name: "model with its variant", models: models, provider: "outprov", model: "m1", variant: "high"},
		{name: "model id with slashes", models: models, provider: "other", model: "x/with/slashes", variant: "max"},
		{name: "unknown provider names the providers", models: models, provider: "nope", model: "m1",
			wantErr: []string{`no provider "nope"`, "other, outprov"}},
		{name: "unknown model names the provider's models", models: models, provider: "outprov", model: "zzz",
			wantErr: []string{`provider "outprov" has no model "zzz"`, "m1, m2"}},
		{name: "unknown variant names the model's variants", models: models, provider: "outprov", model: "m1", variant: "nope",
			wantErr: []string{`outprov/m1 has no variant "nope"`, "high, low"}},
		{name: "a model with no variants says so", models: models, provider: "outprov", model: "m2", variant: "high",
			wantErr: []string{`outprov/m2 has no variant "high"`, "declares none"}},
		{name: "no models at all", models: nil, provider: "outprov", model: "m1",
			wantErr: []string{"lists no models", "/connect"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckModel(c.models, c.provider, c.model, c.variant)
			if len(c.wantErr) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range c.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}

func TestCheckModel_LongListsAreCut(t *testing.T) {
	var models []Model
	for i := 0; i < 40; i++ {
		models = append(models, Model{ProviderID: "p", ID: "m" + string(rune('a'+i%26)) + string(rune('a'+i/26))})
	}
	err := CheckModel(models, "p", "missing", "")
	if err == nil || !strings.Contains(err.Error(), "and 15 more") {
		t.Errorf("err = %v, want the list cut at 25 with a count of the rest", err)
	}
}
