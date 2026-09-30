package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// SavedScene is the on-disk sidecar for one diagram's whiteboard state:
// source_hash pins it to the Mermaid source it was converted from (or last
// diverged from), so a later restore/prompt/reconvert decision can be made
// without re-parsing anything (see resolveWhiteboardInitAction in
// whiteboard-core.js, ported unchanged into the browser bundle). Scene and
// Baseline are opaque Excalidraw JSON as produced by the whiteboard frame;
// vexillum's Go side never needs to interpret their structure, only persist
// and hand them back.
type SavedScene struct {
	SourceHash         string          `json:"source_hash"`
	TextMetricsVersion int             `json:"text_metrics_version"`
	UpdatedAt          time.Time       `json:"updated_at"`
	Scene              json.RawMessage `json:"scene"`
	Baseline           json.RawMessage `json:"baseline"`
}

// FeedbackFiles are the paths written by WriteFeedbackFiles.
type FeedbackFiles struct {
	ScenePath   string
	PreviewPath string
}

// Store persists whiteboard scenes and published feedback under
// <projectRoot>/reviews/<key>/whiteboards/, one JSON file per (session key,
// diagram index) plus the published `.excalidraw`/`.png` pair a queued
// feedback writes. Kept out of any single combined state file on purpose:
// a multi-hundred-KB Excalidraw scene autosaving every second would turn
// every unrelated write into a large rewrite if it shared one file with
// anything else.
//
// A Store instance is process-local to one `vexillum review` invocation
// serving one browser tab, so a single mutex serializing its writes (rather
// than a per-index write queue) is enough to prevent interleaved writes to
// the same file without adding concurrency machinery this scope doesn't
// need.
type Store struct {
	projectRoot string
	mu          sync.Mutex
}

// NewStore returns a Store rooted at projectRoot (an
// internal/project.Root(...) result).
func NewStore(projectRoot string) *Store {
	return &Store{projectRoot: projectRoot}
}

func (s *Store) dir(key string) string {
	return filepath.Join(s.projectRoot, "reviews", key, "whiteboards")
}

func (s *Store) workingFile(key string, index int) string {
	return filepath.Join(s.dir(key), fmt.Sprintf("%d.json", index))
}

// FeedbackPaths returns where WriteFeedbackFiles will write the published
// `.excalidraw` scene and its PNG preview for (key, index).
func (s *Store) FeedbackPaths(key string, index int) (FeedbackFiles, error) {
	if err := validateRef(key, index); err != nil {
		return FeedbackFiles{}, err
	}
	dir := s.dir(key)
	return FeedbackFiles{
		ScenePath:   filepath.Join(dir, fmt.Sprintf("%d.excalidraw", index)),
		PreviewPath: filepath.Join(dir, fmt.Sprintf("%d.png", index)),
	}, nil
}

// SaveScene persists the working state for (key, index): the editable
// scene, the conversion baseline used for edit summaries, and the hash of
// the Mermaid source the scene was converted from. appState's theme and
// viewBackgroundColor are stripped before writing - the frame always
// derives those from its own init message, and persisting a stale one
// would fight a future restore that opens in a different theme.
func (s *Store) SaveScene(key string, index int, sourceHash string, textMetricsVersion int, scene, baseline json.RawMessage) error {
	if err := validateRef(key, index); err != nil {
		return err
	}
	sanitizedScene, err := sanitizeSceneAppState(scene)
	if err != nil {
		return fmt.Errorf("sanitizing scene for %s/%d: %w", key, index, err)
	}
	record := SavedScene{
		SourceHash:         sourceHash,
		TextMetricsVersion: textMetricsVersion,
		UpdatedAt:          time.Now().UTC(),
		Scene:              sanitizedScene,
		Baseline:           baseline,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir(key), 0o755); err != nil {
		return fmt.Errorf("creating whiteboard dir for %s: %w", key, err)
	}
	if err := atomicfile.WriteJSON(s.workingFile(key, index), record); err != nil {
		return fmt.Errorf("saving whiteboard scene %s/%d: %w", key, index, err)
	}
	return nil
}

// LoadScene returns the saved state for (key, index), or nil if none has
// been saved yet.
func (s *Store) LoadScene(key string, index int) (*SavedScene, error) {
	if err := validateRef(key, index); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.workingFile(key, index))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("loading whiteboard scene %s/%d: %w", key, index, err)
	}
	var record SavedScene
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("parsing whiteboard scene %s/%d: %w", key, index, err)
	}
	return &record, nil
}

// WriteFeedbackFiles publishes the agent/reviewer-facing feedback for a
// queued diagram: a standalone `.excalidraw` scene document and its PNG
// preview (pngData is raw decoded bytes, already stripped of the
// data:image/png;base64, prefix by the caller). Called at queue time so the
// paths returned always point at the exact reviewed state, not whatever a
// later autosave might overwrite.
func (s *Store) WriteFeedbackFiles(key string, index int, scene json.RawMessage, pngData []byte) (FeedbackFiles, error) {
	paths, err := s.FeedbackPaths(key, index)
	if err != nil {
		return FeedbackFiles{}, err
	}
	sceneDoc, err := excalidrawDocument(scene)
	if err != nil {
		return FeedbackFiles{}, fmt.Errorf("building excalidraw document for %s/%d: %w", key, index, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir(key), 0o755); err != nil {
		return FeedbackFiles{}, fmt.Errorf("creating whiteboard dir for %s: %w", key, err)
	}
	if err := atomicfile.WriteJSON(paths.ScenePath, sceneDoc); err != nil {
		return FeedbackFiles{}, fmt.Errorf("writing feedback scene %s/%d: %w", key, index, err)
	}
	if len(pngData) == 0 {
		return FeedbackFiles{ScenePath: paths.ScenePath}, nil
	}
	if err := atomicfile.Write(paths.PreviewPath, pngData); err != nil {
		return FeedbackFiles{}, fmt.Errorf("writing feedback preview %s/%d: %w", key, index, err)
	}
	return paths, nil
}

// excalidrawSceneEnvelope is the shape excalidraw.com (and vexillum's own
// whiteboard frame) recognizes as a standalone `.excalidraw` document.
type excalidrawSceneEnvelope struct {
	Type     string          `json:"type"`
	Version  int             `json:"version"`
	Source   string          `json:"source"`
	Elements json.RawMessage `json:"elements"`
	AppState json.RawMessage `json:"appState"`
	Files    json.RawMessage `json:"files"`
}

func excalidrawDocument(scene json.RawMessage) (excalidrawSceneEnvelope, error) {
	sanitized, err := sanitizeSceneAppState(scene)
	if err != nil {
		return excalidrawSceneEnvelope{}, err
	}
	var parsed struct {
		Elements json.RawMessage `json:"elements"`
		AppState json.RawMessage `json:"appState"`
		Files    json.RawMessage `json:"files"`
	}
	if len(sanitized) > 0 && string(sanitized) != "null" {
		if err := json.Unmarshal(sanitized, &parsed); err != nil {
			return excalidrawSceneEnvelope{}, fmt.Errorf("parsing scene: %w", err)
		}
	}
	doc := excalidrawSceneEnvelope{
		Type:     "excalidraw",
		Version:  2,
		Source:   "vexillum-review",
		Elements: parsed.Elements,
		AppState: parsed.AppState,
		Files:    parsed.Files,
	}
	if len(doc.Elements) == 0 {
		doc.Elements = json.RawMessage("[]")
	}
	if len(doc.AppState) == 0 {
		doc.AppState = json.RawMessage("{}")
	}
	if len(doc.Files) == 0 {
		doc.Files = json.RawMessage("{}")
	}
	return doc, nil
}

// sanitizeSceneAppState strips appState.theme and appState.viewBackgroundColor
// from scene, mirroring sanitizeWhiteboardScene/sanitizeWhiteboardAppState in
// whiteboard-core.js. A nil or non-object scene passes through unchanged.
func sanitizeSceneAppState(scene json.RawMessage) (json.RawMessage, error) {
	if len(scene) == 0 || string(scene) == "null" {
		return scene, nil
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(scene, &generic); err != nil {
		// Not a JSON object (e.g. the client sent something malformed) -
		// leave it as-is; the frame's own restore() defensively repairs a
		// bad scene on load.
		return scene, nil //nolint:nilerr
	}
	appStateRaw, ok := generic["appState"]
	if !ok {
		return scene, nil
	}
	var appState map[string]json.RawMessage
	if err := json.Unmarshal(appStateRaw, &appState); err != nil {
		return scene, nil //nolint:nilerr
	}
	delete(appState, "theme")
	delete(appState, "viewBackgroundColor")
	sanitizedAppState, err := json.Marshal(appState)
	if err != nil {
		return nil, fmt.Errorf("re-encoding appState: %w", err)
	}
	generic["appState"] = sanitizedAppState
	out, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("re-encoding scene: %w", err)
	}
	return out, nil
}
