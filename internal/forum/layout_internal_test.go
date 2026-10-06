package forum

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"testing"
)

func report(version string, width float64, complete bool, findings ...AuditFinding) AuditReport {
	return AuditReport{ArtifactVersion: version, Complete: complete, TargetPresenceComplete: complete, ViewportWidth: width, Findings: findings}
}

func clipped(selector string, px float64) AuditFinding {
	return AuditFinding{Kind: "clipped-text", Selector: selector, Axis: "horizontal", OverflowPx: px}
}

func only(t *testing.T, in *layoutInbox) layoutIssue {
	t.Helper()
	if len(in.Issues) != 1 {
		t.Fatalf("issues = %+v, want exactly one", in.Issues)
	}
	return in.Issues[0]
}

func TestLayoutInbox_CreatesOnceAndRepeatChangesNothing(t *testing.T) {
	in := newLayoutInbox()
	if persist, notify := in.record(report("v1", 1200, true, clipped("p", 40))); !persist || !notify {
		t.Fatal("a new issue must persist and notify")
	}
	persist, notify := in.record(report("v1", 1200, true, clipped("p", 40)))
	if persist || notify {
		t.Fatalf("seeing it again changed something: persist=%v notify=%v", persist, notify)
	}
	if got := only(t, &in); got.State != stateFresh || got.Class != "desktop" {
		t.Fatalf("issue = %+v", got)
	}
}

func TestLayoutInbox_AProblemThatGrowsUpdatesInPlace(t *testing.T) {
	in := newLayoutInbox()
	in.record(report("v1", 1200, true, clipped("p", 40)))
	id := only(t, &in).ID
	if _, notify := in.record(report("v1", 1200, true, clipped("p", 400))); !notify {
		t.Fatal("a grown overflow is visible in the explanation")
	}
	if got := only(t, &in); got.ID != id || got.Overflow != 400 {
		t.Fatalf("size alone must not open a second record: %+v", in.Issues)
	}
}

func TestLayoutInbox_SameProblemInDifferentClassesAreDifferentIssues(t *testing.T) {
	in := newLayoutInbox()
	in.record(report("v1", 1200, true, clipped("p", 40)))
	in.record(report("v1", 500, true, clipped("p", 40)))
	in.record(report("v1", 800, true, clipped("p", 40)))
	if len(in.Issues) != 3 {
		t.Fatalf("want one issue per class, got %+v", in.Issues)
	}
	ids := map[string]bool{}
	for _, is := range in.Issues {
		ids[is.ID] = true
	}
	if len(ids) != 3 {
		t.Fatal("ids must differ across classes")
	}
}

func TestViewportClassBoundaries(t *testing.T) {
	for _, c := range []struct {
		w     float64
		class string
		label string
	}{{280, "mobile", "Mobile"}, {640, "mobile", "Mobile"}, {640.5, "compact", "Tablet"}, {1024, "compact", "Tablet"}, {1024.5, "desktop", "Desktop"}, {3000, "desktop", "Desktop"}} {
		got := classOf(c.w)
		if got.id != c.class || got.label != c.label {
			t.Errorf("classOf(%v) = %s/%s, want %s/%s", c.w, got.id, got.label, c.class, c.label)
		}
	}
}

func TestLayoutInbox_DropsGarbageAndUnknownRules(t *testing.T) {
	in := newLayoutInbox()
	bad := []AuditFinding{
		{Kind: "overlapping-nonsense", Selector: "p", Axis: "horizontal", OverflowPx: 40},
		{Kind: "clipped-text", Selector: "p", Axis: "diagonal", OverflowPx: 40},
		{Kind: "clipped-text", Selector: "p", Axis: "horizontal", OverflowPx: math.NaN()},
		{Kind: "clipped-text", Selector: "p", Axis: "horizontal", OverflowPx: math.Inf(1)},
		{Kind: "clipped-text", Selector: "p", Axis: "horizontal", OverflowPx: 0.2},
		{Kind: "clipped-text", Selector: "p", Axis: "horizontal", OverflowPx: 1e9},
		{Kind: "clipped-text", Selector: strings.Repeat("a", maxLayoutSelector+1), Axis: "horizontal", OverflowPx: 40},
		{Kind: "clipped-text", Selector: "", Axis: "horizontal", OverflowPx: 40},  // only the page has no selector
		{Kind: "wide-page", Selector: "div", Axis: "horizontal", OverflowPx: 400}, // the page has no element
		{Kind: "buried-text", Selector: "p", Axis: "vertical", OverflowPx: 40},    // no vertical form
	}
	in.record(report("v1", 1200, true, bad...))
	if len(in.Issues) != 0 {
		t.Fatalf("kept garbage: %+v", in.Issues)
	}
	ok := AuditFinding{Kind: "wide-page", Selector: "", Axis: "horizontal", OverflowPx: 1300}
	in.record(report("v1", 1200, true, ok))
	if len(in.Issues) != 1 {
		t.Fatal("the page-level finding with an empty selector is valid")
	}
}

// A pass narrower than any real device proves nothing, and neither does one
// with no usable version stamp or an absurd width.
func TestLayoutInbox_DropsPassesThatSpeakForNoRealViewer(t *testing.T) {
	for _, c := range []AuditReport{
		report("v1", layoutMinWidth-1, true, clipped("p", 40)),
		report("v1", layoutMaxWidth+1, true, clipped("p", 40)),
		report("v1", math.NaN(), true, clipped("p", 40)),
		report("", 1200, true, clipped("p", 40)),
	} {
		in := newLayoutInbox()
		if persist, notify := in.record(c); persist || notify || len(in.Issues) != 0 || in.Revision != 0 {
			t.Errorf("report %+v changed the inbox: %+v", c, in)
		}
	}
	in := newLayoutInbox()
	in.record(report("v1", layoutMinWidth, true, clipped("p", 40)))
	if len(in.Issues) != 1 {
		t.Error("the floor itself is a real viewport")
	}
	// A dropped narrow pass cannot retire what a wide one found.
	in2 := newLayoutInbox()
	in2.record(report("v1", 400, true, clipped("p", 40)))
	in2.record(report("v2", layoutMinWidth-1, true))
	if only(t, &in2).State != stateFresh {
		t.Error("a pass below the floor resolved an issue")
	}
}

// The conclusive-evidence matrix: only a complete pass, for the issue's own
// class, on a newer version, without the finding, retires it.
func TestLayoutInbox_OnlyConclusiveEvidenceRetires(t *testing.T) {
	setup := func() layoutInbox {
		in := newLayoutInbox()
		in.record(report("v1", 1200, true, clipped("p", 40)))
		return in
	}
	for _, c := range []struct {
		name string
		pass AuditReport
		want issueState
	}{
		{"incomplete pass on a newer version", AuditReport{ArtifactVersion: "v2", Complete: false, TargetPresenceComplete: true, ViewportWidth: 1200}, stateFresh},
		{"target presence incomplete", AuditReport{ArtifactVersion: "v2", Complete: true, TargetPresenceComplete: false, ViewportWidth: 1200}, stateFresh},
		{"a pass that threw", AuditReport{ArtifactVersion: "v2", ViewportWidth: 1200}, stateFresh},
		{"another viewport class", report("v2", 500, true), stateFresh},
		{"same version", report("v1", 1200, true), stateFresh},
		{"newer version, complete, finding gone", report("v2", 1200, true), stateResolved},
	} {
		in := setup()
		in.record(c.pass)
		if got := only(t, &in).State; got != c.want {
			t.Errorf("%s: state = %s, want %s", c.name, got, c.want)
		}
	}
	// A pass for an older version (a late message) is not newer evidence.
	in := setup()
	in.record(report("v2", 1200, true, clipped("p", 40)))
	in.record(report("v1", 1200, true))
	if only(t, &in).State != stateFresh {
		t.Error("a late pass from an older version retired an issue")
	}
	// Too many findings means the report was cut: not conclusive.
	in = setup()
	flood := make([]AuditFinding, maxFindingsPerReport+1)
	for i := range flood {
		flood[i] = clipped("p"+strings.Repeat("x", i%7)+string(rune('a'+i%26)), 40)
	}
	in.record(report("v2", 1200, true, flood...))
	if in.Issues[0].State == stateResolved {
		t.Error("a truncated report retired an issue")
	}
}

func TestLayoutInbox_QueueingReleaseAndStillPresent(t *testing.T) {
	in := newLayoutInbox()
	in.record(report("v1", 1200, true, clipped("p", 40)))
	id := only(t, &in).ID

	picked := in.pick([]string{id, id, "nope"})
	if len(picked) != 1 {
		t.Fatalf("picked %d", len(picked))
	}
	in.markQueued(picked)
	if got := only(t, &in); got.State != stateAwaitingFix || got.State.selectable() || !got.State.outstanding() {
		t.Fatalf("after queueing: %+v", got)
	}
	if len(in.pick([]string{id})) != 0 {
		t.Fatal("an outstanding issue cannot be queued a second time")
	}
	// Same-version passes and queueing itself do not change it.
	in.record(report("v1", 1200, true, clipped("p", 40)))
	in.record(report("v1", 1200, true))
	if only(t, &in).State != stateAwaitingFix {
		t.Fatal("a same-version pass moved a queued issue")
	}
	// Withdrawn before sending: released and selectable again.
	if !in.release([]string{id}) || only(t, &in).State != stateFresh {
		t.Fatalf("release: %+v", in.Issues)
	}
	// Queue again, then a later conclusive pass still finds it.
	in.markQueued(in.pick([]string{id}))
	in.record(report("v2", 1200, true, clipped("p", 40)))
	got := only(t, &in)
	if got.State != statePersisting || !got.State.selectable() || got.State.statusLabel() != "Still present" {
		t.Fatalf("still present: %+v", got)
	}
	// Releasing something that is not outstanding changes nothing.
	if in.release([]string{id}) {
		t.Fatal("release touched an issue nobody was waiting on")
	}
	// A request released from "still present" returns to it, not to fresh.
	in.markQueued(in.pick([]string{id}))
	in.release([]string{id})
	if only(t, &in).State != statePersisting {
		t.Fatalf("release must restore the earlier state, got %s", in.Issues[0].State)
	}
}

// (a) queued, next pass inconclusive, later resolved either way.
func TestLayoutInbox_AwaitingCheckResolvesBothWays(t *testing.T) {
	queue := func() layoutInbox {
		in := newLayoutInbox()
		in.record(report("v1", 1200, true, clipped("p", 40)))
		in.markQueued(in.pick([]string{in.Issues[0].ID}))
		in.record(report("v2", 1200, false)) // the page did not settle
		return in
	}
	in := queue()
	if got := only(t, &in); got.State != stateAwaitingCheck || got.State.selectable() || got.State.statusLabel() == "" {
		t.Fatalf("inconclusive next pass: %+v", got)
	}
	in.record(report("v2", 1200, false, clipped("p", 40))) // inconclusive again: still waiting
	if only(t, &in).State != stateAwaitingCheck {
		t.Fatal("a second inconclusive pass changed the state")
	}
	fixed := queue()
	fixed.record(report("v2", 1200, true))
	if only(t, &fixed).State != stateResolved {
		t.Fatalf("a conclusive pass without it resolves: %s", fixed.Issues[0].State)
	}
	still := queue()
	still.record(report("v2", 1200, true, clipped("p", 40)))
	if only(t, &still).State != statePersisting {
		t.Fatalf("a conclusive pass with it is Still present: %s", still.Issues[0].State)
	}
	// An inconclusive pass for another class says nothing about a queued issue.
	other := newLayoutInbox()
	other.record(report("v1", 1200, true, clipped("p", 40)))
	other.markQueued(other.pick([]string{other.Issues[0].ID}))
	other.record(report("v2", 500, false))
	if only(t, &other).State != stateAwaitingFix {
		t.Fatal("a phone-width pass moved a desktop request")
	}
}

// (b) retired, then a later version shows it again.
func TestLayoutInbox_ARetiredIssueThatReturnsIsDistinct(t *testing.T) {
	in := newLayoutInbox()
	in.record(report("v1", 1200, true, clipped("p", 40)))
	in.record(report("v2", 1200, true))
	if only(t, &in).State != stateResolved {
		t.Fatal("setup")
	}
	views := in.views()
	if len(views) != 1 || views[0].Active || views[0].StatusLabel != "Resolved" || views[0].Selectable {
		t.Fatalf("a resolved issue is shown closed: %+v", views)
	}
	in.record(report("v3", 1200, true, clipped("p", 40)))
	got := only(t, &in)
	if got.State != stateRegressed || !got.State.selectable() || got.State.statusLabel() != "Came back" {
		t.Fatalf("returned: %+v", got)
	}
	if v := in.views()[0]; !v.Active || v.StatusLabel != "Came back" || !v.Selectable {
		t.Fatalf("view of a returned issue: %+v", v)
	}
}

func TestLayoutInbox_DismissIsScopedToTheVersionOnScreen(t *testing.T) {
	in := newLayoutInbox()
	in.record(report("v1", 1200, true, clipped("p", 40)))
	id := in.Issues[0].ID
	if !in.dismiss(id) {
		t.Fatal("dismiss")
	}
	if in.dismiss(id) {
		t.Fatal("dismissing twice must report no change")
	}
	if len(in.views()) != 0 {
		t.Fatal("dismissed issues are not shown")
	}
	in.record(report("v1", 1200, true, clipped("p", 40)))
	in.record(report("v1", 1200, true))
	if only(t, &in).State != stateDismissed {
		t.Fatal("the same version must not undo a dismissal")
	}
	in.record(report("v2", 1200, true, clipped("p", 40)))
	if only(t, &in).State != stateFresh {
		t.Fatal("a newer version that still shows it brings it back")
	}
	// A dismissed issue gone in a newer version just stays quiet.
	in2 := newLayoutInbox()
	in2.record(report("v1", 1200, true, clipped("p", 40)))
	in2.dismiss(in2.Issues[0].ID)
	in2.record(report("v2", 1200, true))
	if got := in2.Issues[0]; got.State != stateDismissed || len(in2.views()) != 0 {
		t.Fatalf("dismissed then fixed: %+v", got)
	}
	// Outstanding issues cannot be dismissed from under their request.
	in3 := newLayoutInbox()
	in3.record(report("v1", 1200, true, clipped("p", 40)))
	in3.markQueued(in3.pick([]string{in3.Issues[0].ID}))
	if in3.dismiss(in3.Issues[0].ID) {
		t.Fatal("dismissed an issue with a request out")
	}
}

func TestLayoutInbox_RevisionsStayMonotonicWhenVersionsAreForgotten(t *testing.T) {
	in := newLayoutInbox()
	var last int
	for i := 0; i < maxLayoutStamps*2; i++ {
		rev, added := in.revisionFor("v" + string(rune('A'+i%26)) + strings.Repeat("x", i))
		if !added || rev <= last {
			t.Fatalf("revision %d after %d (added=%v)", rev, last, added)
		}
		last = rev
	}
	if len(in.Stamps) != maxLayoutStamps {
		t.Fatalf("stamps = %d, want %d", len(in.Stamps), maxLayoutStamps)
	}
	// The oldest version was forgotten; seeing it again is a newer revision.
	rev, added := in.revisionFor("vA")
	if !added || rev <= last {
		t.Fatalf("a forgotten version came back as revision %d (last %d)", rev, last)
	}
	// A remembered one keeps its number.
	again, added := in.revisionFor("vA")
	if added || again != rev {
		t.Fatal("a remembered version must keep its revision")
	}
}

func TestLayoutInbox_BoundsAreEnforced(t *testing.T) {
	in := newLayoutInbox()
	// Live issues stop at the cap and new ones are refused, not evicting live ones.
	var findings []AuditFinding
	for i := 0; i < maxLayoutIssues+30; i++ {
		findings = append(findings, clipped("div:nth-of-type("+itoa(i+1)+")", 40))
	}
	for start := 0; start < len(findings); start += 50 {
		end := min(start+50, len(findings))
		in.record(report("v1", 1200, true, findings[start:end]...))
	}
	if len(in.Issues) != maxLayoutIssues {
		t.Fatalf("issues = %d, want the cap %d", len(in.Issues), maxLayoutIssues)
	}
	// Resolved history is capped and dropped first when room is needed.
	in2 := newLayoutInbox()
	for i := 0; i < maxResolvedIssues+10; i++ {
		sel := "p:nth-of-type(" + itoa(i+1) + ")"
		in2.record(report("a"+itoa(i), 1200, true, clipped(sel, 40)))
		in2.record(report("b"+itoa(i), 1200, true))
	}
	resolved := 0
	for _, is := range in2.Issues {
		if is.State == stateResolved {
			resolved++
		}
	}
	if resolved > maxResolvedIssues {
		t.Fatalf("resolved = %d, want at most %d", resolved, maxResolvedIssues)
	}
	// Dismissed history is capped too.
	in3 := newLayoutInbox()
	for i := 0; i < maxDismissedIssues+10; i++ {
		in3.record(report("v1", 1200, true, clipped("p:nth-of-type("+itoa(i+1)+")", 40)))
		in3.dismiss(issueID("clipped-text", "p:nth-of-type("+itoa(i+1)+")", "desktop"))
	}
	if n := len(in3.Issues); n > maxDismissedIssues {
		t.Fatalf("dismissed kept %d", n)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestIssueID_ShapeAndSameness(t *testing.T) {
	id := issueID("clipped-text", "div#a > p", "desktop")
	if !regexp.MustCompile(`^[a-z2-7]{16}$`).MatchString(id) || !validIssueID(id) {
		t.Fatalf("id %q is not 16 url-safe characters", id)
	}
	if id != issueID("clipped-text", "div#a > p", "desktop") {
		t.Fatal("the same sighting must give the same id")
	}
	for _, other := range []string{issueID("buried-text", "div#a > p", "desktop"), issueID("clipped-text", "div#a > p", "mobile"), issueID("clipped-text", "div#a > q", "desktop")} {
		if other == id {
			t.Fatal("different sightings must differ")
		}
	}
	// Length-prefixing: moving a character across a field boundary changes the id.
	if issueID("ab", "c", "desktop") == issueID("a", "bc", "desktop") {
		t.Fatal("fields must not run together")
	}
	if validIssueID("42eb3759445106dc") || validIssueID(id+"a") {
		t.Fatal("validIssueID accepted a non-id")
	}
}

func TestLayoutInbox_PersistenceRoundTripAndVersionHandling(t *testing.T) {
	in := newLayoutInbox()
	in.record(report("v1", 1200, true, clipped("p", 40)))
	in.record(report("v1", 500, true, clipped("p", 40)))
	in.markQueued(in.pick([]string{in.Issues[0].ID}))
	home := t.TempDir()
	key := "0123456789abcdef"
	if err := saveLayout(home, key, &in); err != nil {
		t.Fatal(err)
	}
	back, err := loadLayout(home, key)
	if err != nil {
		t.Fatal(err)
	}
	if back.Revision != in.Revision || len(back.Issues) != 2 || back.Issues[0].State != stateAwaitingFix || len(back.Stamps) != 1 {
		t.Fatalf("round trip lost state: %+v", back)
	}
	for _, old := range []string{`{"version":1,"warnings":[{"id":"x"}]}`, `{"version":99,"issues":[]}`, `{"issues":[]}`} {
		got, err := decodeLayoutInbox([]byte(old))
		if err != nil || got.Version != layoutFileVersion || len(got.Issues) != 0 || got.Revision != 0 {
			t.Errorf("%s: want an empty inbox, got %+v %v", old, got, err)
		}
	}
	if _, err := decodeLayoutInbox([]byte("not json")); err == nil {
		t.Error("corrupt JSON is an error the hub logs and recovers from")
	}
	// Records in a current-version file are re-validated.
	tampered := `{"version":2,"revision":3,"stamps":[],"issues":[{"id":"nope","kind":"clipped-text","selector":"p","axis":"horizontal","overflow_px":9,"class":"desktop","width":1000,"state":"fresh","seen_rev":1}]}`
	got, _ := decodeLayoutInbox([]byte(tampered))
	if len(got.Issues) != 0 {
		t.Fatal("a record whose id does not derive from its fields was kept")
	}
}

func TestLayoutPrompt_ShorteningAndTooLarge(t *testing.T) {
	var issues []layoutIssue
	for i := 0; i < 40; i++ {
		sel := strings.Repeat("section > ", 28) + "p:nth-of-type(" + itoa(i+1) + ")"
		issues = append(issues, layoutIssue{ID: issueID("clipped-text", sel, "desktop"), Kind: "clipped-text", Selector: sel, Axis: "horizontal", Overflow: 40, Class: "desktop", Width: 1200, State: stateFresh})
	}
	used, body, label, target, err := composeLayoutPrompt(issues)
	if err != nil {
		t.Fatal(err)
	}
	if len(used) >= len(issues) || len(used) == 0 {
		t.Fatalf("a batch past the target limit must be shortened, used %d of %d", len(used), len(issues))
	}
	if len(target) > maxTargetBytes || len(body) > maxPromptChars {
		t.Fatalf("shortened batch still too large: %d / %d", len(target), len(body))
	}
	if !strings.HasPrefix(body, "The browser flagged "+itoa(len(used))+" layout problems") || label != "Layout issues: "+itoa(len(used))+" selected" {
		t.Fatalf("header and label must count the issues actually used: %q / %q", strings.SplitN(body, "\n", 2)[0], label)
	}
	// Not even one fits.
	huge := layoutIssue{Kind: "clipped-text", Selector: strings.Repeat("a", 9000), Axis: "horizontal", Overflow: 40, Class: "desktop", Width: 1200, State: stateFresh}
	if _, _, _, _, err := composeLayoutPrompt([]layoutIssue{huge}); err != ErrIssueTooLargeToQueue {
		t.Fatalf("err = %v, want ErrIssueTooLargeToQueue", err)
	}
}

func TestLayoutPrompt_StatusWordsAndPageSelector(t *testing.T) {
	mk := func(state issueState) layoutIssue {
		return layoutIssue{ID: issueID("wide-page", "", "desktop"), Kind: "wide-page", Axis: "horizontal", Overflow: 1300, Class: "desktop", Width: 1100, State: state}
	}
	for state, want := range map[issueState]string{stateFresh: "Status: Open.", statePersisting: "Status: Still present.", stateRegressed: "Status: Came back."} {
		_, body, _, _, err := composeLayoutPrompt([]layoutIssue{mk(state)})
		if err != nil || !strings.Contains(body, want) {
			t.Errorf("%s: %v\n%s", state, err, body)
		}
		if !strings.Contains(body, `Selector: "html".`) {
			t.Errorf("the page itself is quoted as html:\n%s", body)
		}
	}
}

func TestExplanations_AreShortSpecificAndNeverPrescribe(t *testing.T) {
	for _, r := range issueRules {
		for _, axis := range []string{"horizontal", "vertical"} {
			for _, px := range []float64{-1234.5, 7, 99999} {
				text := r.explain(axis, px, 1106)
				if len(text) >= 140 || len(r.title) > 40 {
					t.Errorf("%s: too long (%d chars, title %d): %q", r.id, len(text), len(r.title), text)
				}
				if !regexp.MustCompile(`\d+px`).MatchString(text) {
					t.Errorf("%s: no measurement in %q", r.id, text)
				}
				for _, banned := range []string{"should", "fix", "change", "use "} {
					if strings.Contains(strings.ToLower(text), banned) {
						t.Errorf("%s: prescribes (%q) in %q", r.id, banned, text)
					}
				}
			}
		}
	}
	if got := issueRules[0].explain("horizontal", 549.9, 0); got != "On the right side, this text runs 550px beyond its container and is clipped." {
		t.Fatalf("pinned sentence: %q", got)
	}
	if got := issueRules[0].explain("horizontal", -30, 0); !strings.Contains(got, "left side, this text runs 30px") {
		t.Fatalf("left side: %q", got)
	}
	if got := issueRules[0].explain("vertical", 20, 0); !strings.Contains(got, "bottom side") {
		t.Fatalf("bottom side: %q", got)
	}
	ids := map[string]bool{}
	titles := map[string]bool{}
	for _, r := range issueRules {
		if ids[r.id] || titles[r.title] {
			t.Fatalf("duplicate rule %s / %s", r.id, r.title)
		}
		ids[r.id], titles[r.title] = true, true
	}
}

func TestPromptSelector_FlattensAndEscapes(t *testing.T) {
	got := promptSelector("a\"b\\c\nd\r\te f g\x00h")
	if strings.ContainsAny(got, "\n\r\t  \x00") {
		t.Fatalf("not flattened: %q", got)
	}
	if got != `a\"b\\c d  e f g h` {
		t.Fatalf("got %q", got)
	}
	long := promptSelector(strings.Repeat("é", 500))
	if r := []rune(long); len(r) != promptSelectorRunes+3 || !strings.HasSuffix(long, "...") {
		t.Fatalf("bound: %d runes", len(r))
	}
	if promptSelector("") != "html" {
		t.Fatal("the page")
	}
}

func TestUnreferencedLayoutIDs_AnotherPendingPromptKeepsTheIssue(t *testing.T) {
	l := &liveSession{rec: sessionRecord{
		Queued: []Prompt{{UID: "b", LayoutIDs: []string{"w2", "w3"}}},
		Outbox: []Prompt{{UID: "c", LayoutIDs: []string{"w4"}}},
	}}
	got := l.unreferencedLayoutIDs([]string{"w1", "w2", "w4", "w5"})
	if strings.Join(got, ",") != "w1,w5" {
		t.Fatalf("got %v", got)
	}
}
