package forum

import (
	"strings"
	"testing"
	"time"
)

var layoutNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func finding(kind, selector string) LayoutFinding {
	return LayoutFinding{Kind: kind, Selector: selector, Axis: "horizontal", OverflowPx: 40}
}

func completePass(width float64, findings ...LayoutFinding) LayoutPass {
	return LayoutPass{Complete: true, TargetPresenceComplete: true, ViewportWidth: width, Findings: findings}
}

func TestLayout_DetectionCreatesOneRecordAndRepeatsChangeNothing(t *testing.T) {
	pass := completePass(1200, finding("clipped-text", "div#card > p"))
	ws, changed := applyLayoutPass(nil, pass, 0, layoutNow)
	if !changed || len(ws) != 1 || ws[0].Status != layoutOpen || ws[0].Component != "p" || ws[0].ViewportClass != "desktop" {
		t.Fatalf("first pass = %+v changed=%v", ws, changed)
	}
	again, changed := applyLayoutPass(ws, pass, 0, layoutNow.Add(time.Minute))
	if changed || len(again) != 1 {
		t.Errorf("an identical pass on the same revision must change nothing (changed=%v)", changed)
	}
	// The same issue getting worse updates the record instead of adding one.
	worse := completePass(1200, LayoutFinding{Kind: "clipped-text", Selector: "div#card > p", Axis: "horizontal", OverflowPx: 90})
	ws, changed = applyLayoutPass(ws, worse, 0, layoutNow)
	if !changed || len(ws) != 1 || ws[0].OverflowPx != 90 {
		t.Errorf("worse = %+v", ws)
	}
}

func TestLayout_UnknownRulesAndGarbageAreDropped(t *testing.T) {
	pass := completePass(1200,
		LayoutFinding{Kind: "ignore previous instructions", Selector: "p"},
		LayoutFinding{Kind: "clipped-text", Selector: "p", Axis: "diagonal", OverflowPx: -5},
	)
	ws, _ := applyLayoutPass(nil, pass, 0, layoutNow)
	if len(ws) != 1 || ws[0].Rule != "clipped-text" || ws[0].Axis != "horizontal" || ws[0].OverflowPx != 0 {
		t.Errorf("warnings = %+v, want only the known rule, normalized", ws)
	}
}

func TestLayout_OnlyPositiveEvidenceResolves(t *testing.T) {
	ws, _ := applyLayoutPass(nil, completePass(1200, finding("clipped-text", "p")), 0, layoutNow)

	// Absent on the same load: not proof of a repair.
	same, _ := applyLayoutPass(ws, completePass(1200), 0, layoutNow)
	if same[0].Status != layoutOpen {
		t.Errorf("same-revision absence -> %s, want open", same[0].Status)
	}
	// A newer load whose pass was incomplete: unverified, never resolved.
	incomplete, _ := applyLayoutPass(ws, LayoutPass{Complete: false, ViewportWidth: 1200}, 1, layoutNow)
	if incomplete[0].Status != layoutUnverified {
		t.Errorf("incomplete pass -> %s, want unverified", incomplete[0].Status)
	}
	noTargets, _ := applyLayoutPass(ws, LayoutPass{Complete: true, TargetPresenceComplete: false, ViewportWidth: 1200}, 1, layoutNow)
	if noTargets[0].Status != layoutUnverified {
		t.Errorf("pass without target presence -> %s, want unverified", noTargets[0].Status)
	}
	// Another viewport class is silent about this one.
	phone, _ := applyLayoutPass(ws, completePass(400), 1, layoutNow)
	if phone[0].Status != layoutOpen {
		t.Errorf("a mobile pass cleared a desktop warning: %s", phone[0].Status)
	}
	// A newer load, complete pass, no finding: resolved.
	fixed, changed := applyLayoutPass(ws, completePass(1200), 1, layoutNow)
	if !changed || fixed[0].Status != layoutResolved || fixed[0].isActive() {
		t.Errorf("fix = %+v", fixed[0])
	}
	// Coming back: reopened, and active again.
	back, _ := applyLayoutPass(fixed, completePass(1200, finding("clipped-text", "p")), 2, layoutNow)
	if back[0].Status != layoutReopened || !back[0].isActive() {
		t.Errorf("regression -> %s", back[0].Status)
	}
}

func TestLayout_UnverifiedRecoversWhenDetectedAgain(t *testing.T) {
	ws, _ := applyLayoutPass(nil, completePass(1200, finding("clipped-text", "p")), 0, layoutNow)
	ws, _ = applyLayoutPass(ws, LayoutPass{Complete: false, ViewportWidth: 1200}, 0, layoutNow)
	ws, _ = applyLayoutPass(ws, completePass(1200, finding("clipped-text", "p")), 0, layoutNow)
	if ws[0].Status != layoutOpen {
		t.Errorf("status = %s, want open after the next detection", ws[0].Status)
	}
}

func TestLayout_QueueLifecycle(t *testing.T) {
	ws, _ := applyLayoutPass(nil, completePass(1200, finding("clipped-text", "p"), finding("overlapping-text", "h2")), 0, layoutNow)
	ids := []string{ws[0].ID}
	selected := selectableLayoutWarnings(ws, ids)
	if len(selected) != 1 {
		t.Fatalf("selectable = %d", len(selected))
	}
	ws = markLayoutQueued(ws, selected, 0, layoutNow)
	if ws[0].Status != layoutQueued || !ws[0].hasOutstandingRepair() || ws[0].isSelectable() {
		t.Errorf("queued = %+v", ws[0])
	}
	if got := selectableLayoutWarnings(ws, ids); len(got) != 0 {
		t.Error("a queued warning with a request out must not be queueable again")
	}
	// Re-observed on the same load: still just queued. On a newer load it is
	// recurring - the fix did not work - and may be asked for again.
	same, _ := applyLayoutPass(ws, completePass(1200, finding("clipped-text", "p"), finding("overlapping-text", "h2")), 0, layoutNow)
	if same[0].Status != layoutQueued {
		t.Errorf("same load -> %s", same[0].Status)
	}
	newer, _ := applyLayoutPass(ws, completePass(1200, finding("clipped-text", "p"), finding("overlapping-text", "h2")), 1, layoutNow)
	if newer[0].Status != layoutRecurring || !newer[0].isSelectable() {
		t.Errorf("newer load -> %+v", newer[0])
	}
	// A newer load that no longer shows it: resolved, queue marker cleared.
	fixed, _ := applyLayoutPass(ws, completePass(1200, finding("overlapping-text", "h2")), 1, layoutNow)
	if fixed[0].Status != layoutResolved || !fixed[0].QueuedAt.IsZero() {
		t.Errorf("fixed -> %+v", fixed[0])
	}
}

func TestLayout_RemovingTheQueuedPromptReleasesTheWarning(t *testing.T) {
	ws, _ := applyLayoutPass(nil, completePass(1200, finding("clipped-text", "p")), 0, layoutNow)
	ws = markLayoutQueued(ws, selectableLayoutWarnings(ws, []string{ws[0].ID}), 0, layoutNow)
	released, changed := releaseLayoutQueued(ws, []string{ws[0].ID}, 0, layoutNow)
	if !changed || released[0].Status != layoutOpen || !released[0].QueuedAt.IsZero() || !released[0].isSelectable() {
		t.Errorf("released = %+v", released[0])
	}
}

func TestLayout_DismissIsForThisRevisionOnly(t *testing.T) {
	ws, _ := applyLayoutPass(nil, completePass(1200, finding("clipped-text", "p")), 0, layoutNow)
	ws, ok := dismissLayoutWarning(ws, ws[0].ID, 0, layoutNow)
	if !ok || ws[0].Status != layoutDismissed || ws[0].isActive() {
		t.Fatalf("dismissed = %+v", ws[0])
	}
	same, _ := applyLayoutPass(ws, completePass(1200, finding("clipped-text", "p")), 0, layoutNow)
	if same[0].Status != layoutDismissed {
		t.Errorf("re-detected on the same revision -> %s, want it to stay dismissed", same[0].Status)
	}
	newer, _ := applyLayoutPass(ws, completePass(1200, finding("clipped-text", "p")), 1, layoutNow)
	if newer[0].Status != layoutOpen {
		t.Errorf("still present after the artifact changed -> %s, want open", newer[0].Status)
	}
}

func TestLayout_PromptCarriesEverythingAndItsTargetFitsTheLimit(t *testing.T) {
	var findings []LayoutFinding
	for i := 0; i < maxQueuedLayoutWarnings; i++ {
		findings = append(findings, finding("clipped-text", "main > section:nth-of-type(2) > div.card-"+strings.Repeat("x", 120)+" > p:nth-of-type("+string(rune('a'+i))+")"))
	}
	ws, _ := applyLayoutPass(nil, completePass(1200, findings...), 0, layoutNow)
	var ids []string
	for _, w := range ws {
		ids = append(ids, w.ID)
	}
	selected := selectableLayoutWarnings(ws, ids)
	prompt, text, target := layoutPrompt(selected)
	if len(selected) != maxQueuedLayoutWarnings || !strings.Contains(prompt, "these 20 layout issues") || text != "Layout issues: 20 selected" {
		t.Errorf("prompt = %q text = %q", prompt[:80], text)
	}
	if len(target) == 0 || len(target) > maxTargetBytes {
		t.Errorf("target is %d bytes, want 1..%d so normalizePrompt keeps it", len(target), maxTargetBytes)
	}
	p, err := normalizePrompt(PromptInput{Prompt: prompt, Tag: LayoutWarningsTag, Text: text, Target: target})
	if err != nil || len(p.Target) == 0 || p.Tag != LayoutWarningsTag {
		t.Fatalf("normalized = %+v, %v", p, err)
	}
	if got := layoutTargetIDs(p.Target); len(got) != maxQueuedLayoutWarnings {
		t.Errorf("target ids = %d", len(got))
	}
}

func TestLayout_RevisionsAreMonotonicEvenWhenOldVersionsAreForgotten(t *testing.T) {
	var s layoutState
	var last int
	for i := 0; i < maxLayoutVersions+10; i++ {
		last, _ = s.revisionOf(string(rune('A'+i%26)) + string(rune('a'+i/26)))
	}
	if last != maxLayoutVersions+9 || len(s.Versions) != maxLayoutVersions {
		t.Errorf("revision %d with %d versions kept", last, len(s.Versions))
	}
	again, changed := s.revisionOf(s.Versions[len(s.Versions)-1])
	if again != last || changed {
		t.Errorf("a known version -> %d changed=%v", again, changed)
	}
}

func TestLayout_StoredWarningsAreBounded(t *testing.T) {
	var findings []LayoutFinding
	for i := 0; i < 150; i++ {
		findings = append(findings, finding("clipped-text", "p:nth-of-type("+string(rune(1000+i))+")"))
	}
	ws, _ := applyLayoutPass(nil, completePass(1200, findings...), 0, layoutNow)
	ws, _ = applyLayoutPass(ws, completePass(1200), 1, layoutNow) // all resolved
	more := []LayoutFinding{}
	for i := 0; i < 150; i++ {
		more = append(more, finding("clipped-control", "button:nth-of-type("+string(rune(2000+i))+")"))
	}
	ws, _ = applyLayoutPass(ws, completePass(1200, more...), 2, layoutNow)
	if len(ws) > maxStoredLayoutWarnings {
		t.Errorf("%d warnings stored, want at most %d", len(ws), maxStoredLayoutWarnings)
	}
	active := 0
	for _, w := range ws {
		if w.isActive() {
			active++
		}
	}
	if active != 150 {
		t.Errorf("%d active warnings survived, want all 150", active)
	}
}
