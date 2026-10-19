package soldier_test

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/isaias-alt/vexillum/internal/opencodeapi"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// fakeSessions is the OpenCode service as the V2 launch sees it.
type fakeSessions struct {
	mu sync.Mutex

	ensureErr  error
	models     []opencodeapi.Model
	modelsErr  error
	createErr  error
	sessions   map[string]opencodeapi.Session // by id
	sessionErr error                          // returned by Session for an id that is not in sessions, instead of not-found

	log     []string // every call, in order
	created []createdSession
}

type createdSession struct {
	Directory, Title string
	Model            opencodeapi.SessionModel
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{
		models: []opencodeapi.Model{
			{ProviderID: "outprov", ID: "m1", Variants: []string{"high", "low"}},
			{ProviderID: "outprov", ID: "m2"},
			{ProviderID: "opencode", ID: "glm-5.3-flash-free"},
			{ProviderID: "opencode", ID: "deepseek-v4.1-flash-free", Variants: []string{"high", "max"}},
			{ProviderID: "opencode", ID: "space-bunny-free", Variants: []string{"high"}},
		},
		sessions: map[string]opencodeapi.Session{},
	}
}

func (f *fakeSessions) EnsureService() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, "ensure")
	return f.ensureErr
}

func (f *fakeSessions) Models(directory string) ([]opencodeapi.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, "models "+directory)
	return f.models, f.modelsErr
}

func (f *fakeSessions) CreateSession(directory, title string, model opencodeapi.SessionModel) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, "create")
	if f.createErr != nil {
		return "", f.createErr
	}
	id := fmt.Sprintf("ses_fake%d", len(f.created)+1)
	f.created = append(f.created, createdSession{Directory: directory, Title: title, Model: model})
	f.sessions[id] = opencodeapi.Session{ID: id, Directory: directory, Model: model}
	return id, nil
}

func (f *fakeSessions) Session(id string) (opencodeapi.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, "session "+id)
	if s, ok := f.sessions[id]; ok {
		return s, nil
	}
	if f.sessionErr != nil {
		return opencodeapi.Session{}, f.sessionErr
	}
	return opencodeapi.Session{}, &opencodeapi.HTTPError{Status: 404, Body: `{"_tag":"SessionNotFoundError"}`}
}

func (f *fakeSessions) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.log)
}

// useSessions installs f as the OpenCode service for the test.
func useSessions(t *testing.T, f *fakeSessions) {
	t.Helper()
	t.Cleanup(soldier.SetOpenCodeSessions(f))
}

func prepareV2(t *testing.T, task state.Task, camp string) (soldier.PreparedLaunch, error) {
	t.Helper()
	spec, err := soldier.OpenCodeHarness{Major: 2}.Launch(task)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Prepare == nil {
		t.Fatal("a V2 launch with a model must prepare a session")
	}
	return spec.Prepare(camp)
}

func TestOpenCodeLaunch_V2_PreparesASessionAndWritesNothing(t *testing.T) {
	spec, err := soldier.OpenCodeHarness{Major: 2}.Launch(ocTask(t, 2, "opencode/deepseek-v4.1-flash-free#max", "medium"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Args) != 0 {
		t.Errorf("the session is opened by Prepare, Args = %v", spec.Args)
	}
	if !slices.Equal(envOf(spec.Env), []string{"OPENCODE_DISABLE_AUTOUPDATE=1"}) {
		t.Errorf("Env = %v: only the auto-update switch, no OPENCODE_CONFIG_CONTENT or any other config", spec.Env)
	}
	if spec.Prepare == nil {
		t.Fatal("Prepare is nil")
	}
}

func envOf(env map[string]string) []string {
	var out []string
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	slices.Sort(out)
	return out
}

func TestOpenCodePrepare_StartsTheServiceThenValidatesThenCreatesTheSession(t *testing.T) {
	f := newFakeSessions()
	useSessions(t, f)
	task := ocTask(t, 2, "opencode/deepseek-v4.1-flash-free#max", "medium")

	got, err := prepareV2(t, task, "/camps/1/p")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Args, []string{"--session", "ses_fake1"}) || got.SessionID != "ses_fake1" {
		t.Errorf("prepared = %+v", got)
	}
	if want := []string{"ensure", "models /camps/1/p", "create"}; !slices.Equal(f.calls(), want) {
		t.Errorf("calls = %v, want %v: the service is started before anything else is asked of it", f.calls(), want)
	}
	want := createdSession{
		Directory: "/camps/1/p",
		Title:     "vexillum " + task.ID,
		Model:     opencodeapi.SessionModel{ProviderID: "opencode", ID: "deepseek-v4.1-flash-free", Variant: "max"},
	}
	if len(f.created) != 1 || f.created[0] != want {
		t.Errorf("created = %+v, want %+v", f.created, want)
	}
}

func TestOpenCodePrepare_NoVariantWhenTheProfileHasNone(t *testing.T) {
	f := newFakeSessions()
	useSessions(t, f)
	// The effort label is recorded on the task and never turned into a variant.
	if _, err := prepareV2(t, ocTask(t, 2, "opencode/glm-5.3-flash-free", "high"), "/c"); err != nil {
		t.Fatal(err)
	}
	if f.created[0].Model.Variant != "" {
		t.Errorf("variant = %q, want none", f.created[0].Model.Variant)
	}
}

func TestOpenCodePrepare_ModelIdWithSlashesKeepsEverythingAfterTheFirstOne(t *testing.T) {
	f := newFakeSessions()
	f.models = append(f.models, opencodeapi.Model{ProviderID: "openrouter", ID: "anthropic/claude-3.5-sonnet:free"})
	useSessions(t, f)
	if _, err := prepareV2(t, ocTask(t, 2, "openrouter/anthropic/claude-3.5-sonnet:free", ""), "/c"); err != nil {
		t.Fatal(err)
	}
	m := f.created[0].Model
	if m.ProviderID != "openrouter" || m.ID != "anthropic/claude-3.5-sonnet:free" {
		t.Errorf("model = %+v", m)
	}
}

func TestOpenCodeLaunch_V2_NoModelStartsPlainOnTheProjectsDefaults(t *testing.T) {
	f := newFakeSessions()
	useSessions(t, f)
	spec, err := soldier.OpenCodeHarness{Major: 2}.Launch(ocTask(t, 2, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Prepare != nil || len(spec.Args) != 0 {
		t.Errorf("a task with no model needs no session: %+v", spec)
	}
	if len(f.calls()) != 0 {
		t.Errorf("the service was used: %v", f.calls())
	}
}

func TestOpenCodePrepare_AnUnknownModelFailsNamingWhatExists(t *testing.T) {
	cases := []struct {
		name, model string
		want        []string
	}{
		{"provider", "nopeprov/m1", []string{"cannot start this soldier on nopeprov/m1", `no provider "nopeprov"`, "opencode, outprov"}},
		{"model", "outprov/zzz", []string{"outprov/zzz", `no model "zzz"`, "m1, m2"}},
		{"variant", "outprov/m1#nope", []string{"outprov/m1#nope", `no variant "nope"`, "high, low"}},
		{"variant on a model with none", "outprov/m2#high", []string{`no variant "high"`, "declares none"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeSessions()
			useSessions(t, f)
			_, err := prepareV2(t, ocTask(t, 2, c.model, ""), "/c")
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
			if len(f.created) != 0 {
				t.Error("a session was created for a model that does not exist")
			}
		})
	}
}

func TestOpenCodePrepare_ServiceProblemsAreNamed(t *testing.T) {
	cases := []struct {
		name string
		set  func(*fakeSessions)
		want string
	}{
		{"service will not start", func(f *fakeSessions) { f.ensureErr = errors.New("port in use") }, "background service is not available: port in use"},
		{"models fail", func(f *fakeSessions) { f.modelsErr = errors.New("HTTP 500") }, "listing the models OpenCode can run in the camp: HTTP 500"},
		{"create fails", func(f *fakeSessions) { f.createErr = errors.New("HTTP 500") }, "creating the soldier's OpenCode session: HTTP 500"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeSessions()
			c.set(f)
			useSessions(t, f)
			_, err := prepareV2(t, ocTask(t, 2, "outprov/m1", ""), "/c")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}

func TestOpenCodePrepare_ReusesTheRecordedSessionOfTheSameCamp(t *testing.T) {
	f := newFakeSessions()
	f.sessions["ses_old"] = opencodeapi.Session{ID: "ses_old", Directory: "/camps/1/p"}
	useSessions(t, f)
	task := ocTask(t, 2, "outprov/m1#high", "")
	task.HarnessSession = "ses_old"

	got, err := prepareV2(t, task, "/camps/1/p")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Args, []string{"--session", "ses_old"}) || got.SessionID != "ses_old" {
		t.Errorf("prepared = %+v", got)
	}
	if want := []string{"ensure", "session ses_old"}; !slices.Equal(f.calls(), want) {
		t.Errorf("calls = %v, want %v: nothing is validated or created for a session that is reused", f.calls(), want)
	}
}

func TestOpenCodePrepare_ReplacesASessionTheServiceLost(t *testing.T) {
	f := newFakeSessions()
	useSessions(t, f)
	task := ocTask(t, 2, "outprov/m1#high", "")
	task.HarnessSession = "ses_deleted"

	got, err := prepareV2(t, task, "/camps/1/p")
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "ses_fake1" || !slices.Equal(got.Args, []string{"--session", "ses_fake1"}) {
		t.Errorf("prepared = %+v, want a new session", got)
	}
	if want := []string{"ensure", "session ses_deleted", "models /camps/1/p", "create"}; !slices.Equal(f.calls(), want) {
		t.Errorf("calls = %v, want %v", f.calls(), want)
	}
}

// A session is located in the camp it was created for. A re-dispatch that gets
// another camp must not open it there: the TUI would work in the old,
// discarded camp.
func TestOpenCodePrepare_DoesNotReuseASessionLocatedInAnotherCamp(t *testing.T) {
	f := newFakeSessions()
	f.sessions["ses_old"] = opencodeapi.Session{ID: "ses_old", Directory: "/camps/1/p"}
	useSessions(t, f)
	task := ocTask(t, 2, "outprov/m1", "")
	task.HarnessSession = "ses_old"

	got, err := prepareV2(t, task, "/camps/2/p")
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID == "ses_old" || f.created[0].Directory != "/camps/2/p" {
		t.Errorf("prepared = %+v, created = %+v", got, f.created)
	}
}

func TestOpenCodePrepare_ALookupFailureIsNotTreatedAsAMissingSession(t *testing.T) {
	f := newFakeSessions()
	f.sessionErr = errors.New("connection refused")
	useSessions(t, f)
	task := ocTask(t, 2, "outprov/m1", "")
	task.HarnessSession = "ses_old"

	_, err := prepareV2(t, task, "/c")
	if err == nil || !strings.Contains(err.Error(), "ses_old") || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("error = %v", err)
	}
	if len(f.created) != 0 {
		t.Error("a new session replaced one that may still exist")
	}
}

func TestOpenCodePrepare_ASymlinkedCampPathIsTheSameCamp(t *testing.T) {
	real := t.TempDir()
	link := t.TempDir() + "/link"
	if err := symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	f := newFakeSessions()
	f.sessions["ses_old"] = opencodeapi.Session{ID: "ses_old", Directory: real}
	useSessions(t, f)
	task := ocTask(t, 2, "outprov/m1", "")
	task.HarnessSession = "ses_old"
	got, err := prepareV2(t, task, link)
	if err != nil || got.SessionID != "ses_old" {
		t.Errorf("prepared = %+v, %v", got, err)
	}
}

func symlink(target, link string) error { return os.Symlink(target, link) }
