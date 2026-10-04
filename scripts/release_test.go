package scripts

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitRepo is a throwaway repository used to exercise release-tag.sh.
type gitRepo struct {
	t   *testing.T
	dir string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// Reading the scripts registers them as inputs of the test, so the go test
	// cache is invalidated when they change.
	for _, f := range []string{"release-tag.sh", "ci-green.sh", "prune-canary.sh"} {
		if _, err := os.ReadFile(f); err != nil {
			t.Fatal(err)
		}
	}
	r := &gitRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q")
	r.commit("first")
	return r
}

func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *gitRepo) commit(msg string) {
	r.t.Helper()
	r.git("commit", "-q", "--allow-empty", "-m", msg)
}

func (r *gitRepo) tag(names ...string) {
	r.t.Helper()
	for _, n := range names {
		r.git("tag", "-a", "-m", n, n)
	}
}

// releaseTag runs release-tag.sh inside the repository.
func (r *gitRepo) releaseTag(args ...string) (string, error) {
	r.t.Helper()
	script, err := filepath.Abs("release-tag.sh")
	if err != nil {
		r.t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (r *gitRepo) wantTag(want string, args ...string) {
	r.t.Helper()
	got, err := r.releaseTag(args...)
	if err != nil {
		r.t.Fatalf("release-tag.sh %v failed: %v\n%s", args, err, got)
	}
	if got != want {
		r.t.Errorf("release-tag.sh %v = %q, want %q", args, got, want)
	}
}

func (r *gitRepo) wantFail(wantMsg string, args ...string) {
	r.t.Helper()
	got, err := r.releaseTag(args...)
	if err == nil {
		r.t.Fatalf("release-tag.sh %v succeeded with %q, want failure", args, got)
	}
	if !strings.Contains(got, wantMsg) {
		r.t.Errorf("release-tag.sh %v output missing %q\n%s", args, wantMsg, got)
	}
}

func TestCanaryTagIsNextPatchOfLatestStable(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	// v0.1.10 must beat v0.1.9 (numeric, not lexical, order); rc and canary
	// tags are not stable and must not move the base.
	r.tag("v0.1.0", "v0.1.9", "v0.1.10", "v0.2.0-rc.1", "v0.1.11-canary.20261001.gabcdef0")
	r.wantTag("v0.1.11-canary.20261004.gabc1234", "canary-tag", "--date", "20261004", "--sha", "abc1234")
}

func TestCanaryTagKeepsAlphanumericShaIdentifier(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.tag("v0.1.1")
	// A sha with a leading zero is not valid as a bare semver identifier.
	r.wantTag("v0.1.2-canary.20261004.g0123456", "canary-tag", "--date", "20261004", "--sha", "0123456789")
}

func TestCanaryTagDefaultsToTodayAndHead(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.tag("v0.3.0")
	sha := r.git("rev-parse", "--short=7", "HEAD")
	want := fmt.Sprintf("v0.3.1-canary.%s.g%s", time.Now().UTC().Format("20060102"), sha)
	r.wantTag(want, "canary-tag")
}

func TestCanaryTagRefusesExistingAndBadInput(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.tag("v0.1.1", "v0.1.2-canary.20261004.gabc1234")
	r.wantFail("already exists", "canary-tag", "--date", "20261004", "--sha", "abc1234")
	r.wantFail("invalid date", "canary-tag", "--date", "2026-10-04", "--sha", "abc1234")
	r.wantFail("invalid sha", "canary-tag", "--date", "20261004", "--sha", "xyz")
}

func TestCanaryPending(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.wantTag("true", "canary-pending") // no canary tag yet

	r.tag("v0.1.1-canary.20261001.gabc1234")
	r.wantTag("false", "canary-pending") // the tag is at HEAD

	r.commit("second")
	r.wantTag("true", "canary-pending")
}

func TestCanaryPendingUsesTheNewestCanaryTag(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	run := func(date string, args ...string) {
		r.t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = r.dir
		cmd.Env = append(os.Environ(),
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_DATE="+date, "GIT_AUTHOR_DATE="+date,
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			r.t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// The older tag sorts first by name but the newer one by creation date.
	run("2026-10-01T00:00:00Z", "tag", "-a", "-m", "old", "v0.1.1-canary.20261002.gzzzzzzz")
	r.commit("second")
	run("2026-10-03T00:00:00Z", "tag", "-a", "-m", "new", "v0.1.1-canary.20261001.gaaaaaaa")
	r.wantTag("false", "canary-pending")
}

func TestStableBumpsFromCanary(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.tag("v0.1.0", "v0.1.1", "v0.2.0-rc.1")
	r.wantTag("v0.1.2", "stable", "--ref", "canary", "--bump", "patch")
	r.wantTag("v0.2.0", "stable", "--ref", "canary", "--bump", "minor")
	r.wantTag("v1.0.0", "stable", "--ref", "canary", "--bump", "major")
	r.wantTag("v0.1.2", "stable") // patch is the default
}

func TestStableExplicitVersionWinsOverBump(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.tag("v0.1.1")
	r.wantTag("v0.4.0", "stable", "--ref", "canary", "--bump", "patch", "--version", "0.4.0")
	r.wantTag("v0.4.0", "stable", "--version", "v0.4.0")
	r.wantFail("invalid version", "stable", "--version", "0.4")
	r.wantFail("invalid version", "stable", "--version", "0.4.0-rc.1")
	r.wantFail("invalid version", "stable", "--version", "01.4.0")
	r.wantFail("already exists", "stable", "--version", "0.1.1")
}

func TestStableRcNumbering(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.tag("v0.1.1")
	r.wantTag("v0.2.0-rc.1", "stable", "--ref", "canary", "--bump", "minor", "--rc")
	r.tag("v0.2.0-rc.1", "v0.2.0-rc.2", "v0.2.0-rc.10", "v0.3.0-rc.7")
	r.wantTag("v0.2.0-rc.11", "stable", "--ref", "canary", "--bump", "minor", "--rc")
	r.wantTag("v0.2.0-rc.11", "stable", "--version", "0.2.0", "--rc")
	r.tag("v0.2.0")
	r.wantFail("already exists", "stable", "--version", "0.2.0", "--rc")
}

func TestStableRefRules(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.tag("v0.1.0", "v0.1.1", "v0.2.0", "v0.2.1")

	r.wantFail("ref must be canary or release/vX.Y", "stable", "--ref", "feat/x")
	r.wantFail("ref must be canary or release/vX.Y", "stable", "--ref", "main")
	r.wantFail("invalid release branch", "stable", "--ref", "release/v0")

	// On a release branch the patch bump stays inside the minor line.
	r.wantTag("v0.1.2", "stable", "--ref", "release/v0.1", "--bump", "patch")
	r.wantTag("v0.2.2", "stable", "--ref", "release/v0.2", "--bump", "patch")
	r.wantTag("v0.3.0", "stable", "--ref", "release/v0.3", "--bump", "patch") // first release of a line
	r.wantTag("v0.3.0-rc.1", "stable", "--ref", "release/v0.3", "--bump", "patch", "--rc")
	r.wantFail("one minor line", "stable", "--ref", "release/v0.2", "--bump", "minor")
	r.wantFail("one minor line", "stable", "--ref", "release/v0.2", "--bump", "major")

	r.wantTag("v0.2.5", "stable", "--ref", "release/v0.2", "--version", "0.2.5")
	r.wantFail("does not belong to release/v0.2", "stable", "--ref", "release/v0.2", "--version", "0.3.0")
}

func TestStableVersionMustBeGreaterThanLatestStable(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.tag("v0.1.1", "v0.2.0-rc.1", "v0.3.0-canary.20261001.gabcdef0")

	// A typo below the latest stable is refused, rc or not; the pre-release
	// tags do not count as the latest stable.
	r.wantFail("not greater than the latest stable v0.1.1", "stable", "--ref", "canary", "--version", "0.0.5")
	r.wantFail("not greater than the latest stable v0.1.1", "stable", "--ref", "canary", "--version", "0.0.5", "--rc")
	r.wantFail("not greater than the latest stable v0.1.1", "stable", "--version", "0.1.0")
	r.wantTag("v0.1.2", "stable", "--ref", "canary", "--version", "0.1.2")
	r.wantTag("v0.2.0-rc.2", "stable", "--ref", "canary", "--version", "0.2.0", "--rc")
	r.wantTag("v1.0.0", "stable", "--ref", "canary", "--version", "1.0.0")

	// Numeric, not lexical, comparison.
	r.tag("v0.1.10")
	r.wantFail("not greater than the latest stable v0.1.10", "stable", "--version", "0.1.9")
	r.wantTag("v0.1.11", "stable", "--version", "0.1.11")
}

func TestStableBackportsStayAboveTheirOwnLine(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.tag("v0.1.0", "v0.1.1", "v0.2.0", "v0.2.3")

	// A backport on an older line may be lower than the newest stable overall.
	r.wantTag("v0.1.2", "stable", "--ref", "release/v0.1", "--version", "0.1.2")
	r.wantTag("v0.1.2", "stable", "--ref", "release/v0.1")
	// But not lower than the latest stable of that line.
	r.wantFail("not greater than the latest stable v0.2.3", "stable", "--ref", "release/v0.2", "--version", "0.2.2")
	r.tag("v0.1.2", "v0.1.4")
	r.wantFail("not greater than the latest stable v0.1.4", "stable", "--ref", "release/v0.1", "--version", "0.1.3", "--rc")
}

func TestStableRejectsInvalidBump(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.wantFail("invalid bump", "stable", "--bump", "huge")
}

func TestReleaseTagUnknownCommand(t *testing.T) {
	t.Parallel()
	r := newGitRepo(t)
	r.wantFail("usage", "nope")
}

// ---- ci-green.sh ----

// ghRunsStub plays back one line of STUB_STATE/runs per call (the last line
// repeats), standing in for `gh run list --jq ...`.
const ghRunsStub = `#!/bin/sh
n=$(cat "$STUB_STATE/n" 2>/dev/null || echo 0)
n=$((n+1))
echo "$n" > "$STUB_STATE/n"
echo "$*" >> "$STUB_STATE/gh.log"
total=$(wc -l < "$STUB_STATE/runs")
if [ "$n" -gt "$total" ]; then n=$total; fi
sed -n "${n}p" "$STUB_STATE/runs"
`

func runCIGreen(t *testing.T, runs string, extraEnv ...string) (string, error) {
	t.Helper()
	if _, err := os.ReadFile("ci-green.sh"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	stubs := filepath.Join(root, "stubs")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(stubs, "gh"), ghRunsStub)
	if err := os.WriteFile(filepath.Join(state, "runs"), []byte(runs), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "ci-green.sh", "abc1234")
	cmd.Env = append([]string{
		"PATH=" + stubs + ":/usr/bin:/bin",
		"STUB_STATE=" + state,
		"CI_POLL_SECONDS=0",
	}, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCIGreenSucceedsOnGreenRun(t *testing.T) {
	t.Parallel()
	out, err := runCIGreen(t, "completed success https://example.test/run/1\n")
	mustSucceed(t, out, err)
	mustContain(t, out, "is green for abc1234")
}

func TestCIGreenFailsOnRedRun(t *testing.T) {
	t.Parallel()
	out, err := runCIGreen(t, "completed failure https://example.test/run/1\n")
	mustFail(t, out, err)
	mustContain(t, out, "finished with 'failure'")
}

func TestCIGreenWaitsForARunningCI(t *testing.T) {
	t.Parallel()
	out, err := runCIGreen(t, "queued null u\nin_progress null u\ncompleted success u\n")
	mustSucceed(t, out, err)
	mustContain(t, out, "is queued")
	mustContain(t, out, "is in_progress")
	mustContain(t, out, "is green")
}

func TestCIGreenGivesUpOnAHungCI(t *testing.T) {
	t.Parallel()
	out, err := runCIGreen(t, "in_progress null u\n", "CI_WAIT_SECONDS=0")
	mustFail(t, out, err)
	mustContain(t, out, "still in_progress")
}

func TestCIGreenFailsWhenCIHasNotSeenTheCommit(t *testing.T) {
	t.Parallel()
	out, err := runCIGreen(t, "\n", "CI_NONE_SECONDS=0")
	mustFail(t, out, err)
	mustContain(t, out, "no ci.yml run found")
}

// ---- prune-canary.sh ----

const ghReleasesStub = `#!/bin/sh
echo "$*" >> "$STUB_STATE/gh.log"
case "$1 $2" in
  "release list") cat "$STUB_STATE/releases.json" ;;
  "release delete") ;;
  *) exit 1 ;;
esac
`

type fakeRelease struct {
	TagName      string `json:"tagName"`
	IsPrerelease bool   `json:"isPrerelease"`
	CreatedAt    string `json:"createdAt"`
}

func runPrune(t *testing.T, releases []fakeRelease, extraEnv ...string) (string, string, error) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available")
	}
	if _, err := os.ReadFile("prune-canary.sh"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	stubs := filepath.Join(root, "stubs")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(stubs, "gh"), ghReleasesStub)
	data, err := json.Marshal(releases)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "releases.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Keep the real PATH after the stubs so jq is found.
	cmd := exec.Command("bash", "prune-canary.sh")
	cmd.Env = append([]string{
		"PATH=" + stubs + ":" + os.Getenv("PATH"),
		"STUB_STATE=" + state,
		"NOW_EPOCH=" + fmt.Sprint(pruneNow.Unix()),
	}, extraEnv...)
	out, err := cmd.CombinedOutput()
	log, _ := os.ReadFile(filepath.Join(state, "gh.log"))
	return string(out), string(log), err
}

var pruneNow = time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)

func daysAgo(d int) string { return pruneNow.AddDate(0, 0, -d).Format("2006-01-02T15:04:05Z") }

// pruneFixture has five canary builds plus releases that must never be touched.
func pruneFixture() []fakeRelease {
	return []fakeRelease{
		{"v0.1.2-canary.20261003.g1111111", true, daysAgo(1)},
		{"v0.1.2-canary.20261002.g2222222", true, daysAgo(2)},
		{"v0.1.2-canary.20260914.g3333333", true, daysAgo(20)},
		{"v0.1.2-canary.20260909.g4444444", true, daysAgo(25)},
		{"v0.1.2-canary.20260825.g5555555", true, daysAgo(40)},
		{"v0.2.0-rc.1", true, daysAgo(80)},
		{"v0.1.1", false, daysAgo(90)},
		// Looks like a canary but is a full release: not a pre-release, not ours.
		{"v0.0.9-canary.20260101.gabcdef0", false, daysAgo(200)},
	}
}

func TestPruneIsADryRunByDefault(t *testing.T) {
	t.Parallel()
	out, log, err := runPrune(t, pruneFixture(), "KEEP_COUNT=2", "KEEP_DAYS=10")
	mustSucceed(t, out, err)
	mustContain(t, out, "[dry-run] would delete v0.1.2-canary.20260914.g3333333")
	mustContain(t, out, "[dry-run] would delete v0.1.2-canary.20260909.g4444444")
	mustContain(t, out, "[dry-run] would delete v0.1.2-canary.20260825.g5555555")
	if strings.Contains(log, "release delete") {
		t.Errorf("dry run must not delete anything, gh log:\n%s", log)
	}
}

func TestPruneKeepsYoungOrNewestBuilds(t *testing.T) {
	t.Parallel()
	// 30 days: the 20 and 25 day old builds are young enough, only the 40 day
	// old one goes.
	out, log, err := runPrune(t, pruneFixture(), "DRY_RUN=false", "KEEP_COUNT=2", "KEEP_DAYS=30")
	mustSucceed(t, out, err)
	mustContain(t, log, "release delete v0.1.2-canary.20260825.g5555555 --cleanup-tag --yes")
	if strings.Count(log, "release delete") != 1 {
		t.Errorf("want exactly one deletion, gh log:\n%s", log)
	}
}

func TestPruneKeepsTheNewestCountEvenWhenOld(t *testing.T) {
	t.Parallel()
	// Everything is older than 10 days except two builds, but the newest 4 are
	// kept by count, so only the oldest build is deleted.
	out, log, err := runPrune(t, pruneFixture(), "DRY_RUN=false", "KEEP_COUNT=4", "KEEP_DAYS=10")
	mustSucceed(t, out, err)
	if strings.Count(log, "release delete") != 1 {
		t.Errorf("want exactly one deletion, gh log:\n%s", log)
	}
	mustContain(t, log, "release delete v0.1.2-canary.20260825.g5555555 --cleanup-tag --yes")
}

func TestPruneNeverTouchesStableOrRc(t *testing.T) {
	t.Parallel()
	out, log, err := runPrune(t, pruneFixture(), "DRY_RUN=false", "KEEP_COUNT=0", "KEEP_DAYS=0")
	mustSucceed(t, out, err)
	for _, protected := range []string{"v0.2.0-rc.1", "v0.1.1 ", "v0.0.9-canary.20260101.gabcdef0"} {
		if strings.Contains(log, "release delete "+protected) {
			t.Errorf("%s must never be deleted, gh log:\n%s", protected, log)
		}
	}
	if strings.Count(log, "release delete") != 5 {
		t.Errorf("want the 5 canary pre-releases deleted, gh log:\n%s", log)
	}
}

func TestPruneWithNothingToDo(t *testing.T) {
	t.Parallel()
	out, log, err := runPrune(t, pruneFixture()[:2], "DRY_RUN=false")
	mustSucceed(t, out, err)
	mustContain(t, out, "no canary build to prune")
	if strings.Contains(log, "release delete") {
		t.Errorf("nothing should be deleted, gh log:\n%s", log)
	}
}

func TestPruneRejectsBadDryRunValue(t *testing.T) {
	t.Parallel()
	out, _, err := runPrune(t, pruneFixture(), "DRY_RUN=maybe")
	mustFail(t, out, err)
	mustContain(t, out, "DRY_RUN must be true or false")
}
