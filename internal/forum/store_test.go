package forum_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isaias-alt/vexillum/internal/forum"
)

const testKey = "0123456789abcdef"

func TestStore_SaveThenLoadScene_RoundTrips(t *testing.T) {
	store := forum.NewStore(t.TempDir())

	scene := json.RawMessage(`{"elements":[{"id":"a"}],"appState":{"scrollX":1}}`)
	baseline := json.RawMessage(`{"elements":[{"id":"a"}]}`)
	if err := store.SaveScene(testKey, 0, "abc123", 1, scene, baseline); err != nil {
		t.Fatalf("SaveScene: %v", err)
	}

	loaded, err := store.LoadScene(testKey, 0)
	if err != nil {
		t.Fatalf("LoadScene: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadScene returned nil after a save")
	}
	if loaded.SourceHash != "abc123" {
		t.Errorf("SourceHash = %q, want %q", loaded.SourceHash, "abc123")
	}
	if loaded.TextMetricsVersion != 1 {
		t.Errorf("TextMetricsVersion = %d, want 1", loaded.TextMetricsVersion)
	}
	if loaded.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestStore_LoadScene_MissingReturnsNilNotError(t *testing.T) {
	store := forum.NewStore(t.TempDir())
	loaded, err := store.LoadScene(testKey, 0)
	if err != nil {
		t.Fatalf("LoadScene on missing file: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for a never-saved diagram, got %+v", loaded)
	}
}

func TestStore_SaveScene_StripsThemeAndBackgroundFromAppState(t *testing.T) {
	store := forum.NewStore(t.TempDir())
	scene := json.RawMessage(`{"elements":[],"appState":{"theme":"dark","viewBackgroundColor":"#000","scrollX":5}}`)
	if err := store.SaveScene(testKey, 0, "h", 0, scene, nil); err != nil {
		t.Fatalf("SaveScene: %v", err)
	}

	loaded, err := store.LoadScene(testKey, 0)
	if err != nil {
		t.Fatalf("LoadScene: %v", err)
	}
	var decoded struct {
		AppState map[string]any `json:"appState"`
	}
	if err := json.Unmarshal(loaded.Scene, &decoded); err != nil {
		t.Fatalf("unmarshaling saved scene: %v", err)
	}
	if _, ok := decoded.AppState["theme"]; ok {
		t.Error("expected theme to be stripped from saved appState")
	}
	if _, ok := decoded.AppState["viewBackgroundColor"]; ok {
		t.Error("expected viewBackgroundColor to be stripped from saved appState")
	}
	if decoded.AppState["scrollX"] != float64(5) {
		t.Errorf("expected unrelated appState fields to survive, scrollX = %v", decoded.AppState["scrollX"])
	}
}

func TestStore_SaveScene_InvalidKeyRejected(t *testing.T) {
	store := forum.NewStore(t.TempDir())
	err := store.SaveScene("not-a-valid-key", 0, "h", 0, json.RawMessage(`{}`), nil)
	if err == nil {
		t.Fatal("expected an error for an invalid session key")
	}
}

func TestStore_SaveScene_InvalidIndexRejected(t *testing.T) {
	store := forum.NewStore(t.TempDir())
	err := store.SaveScene(testKey, 1000, "h", 0, json.RawMessage(`{}`), nil)
	if err == nil {
		t.Fatal("expected an error for an out-of-range diagram index")
	}
}

func TestStore_WriteFeedbackFiles_WritesSceneAndPreview(t *testing.T) {
	dir := t.TempDir()
	store := forum.NewStore(dir)

	scene := json.RawMessage(`{"elements":[{"id":"a"}],"appState":{"theme":"dark"}}`)
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	paths, err := store.WriteFeedbackFiles(testKey, 2, scene, png)
	if err != nil {
		t.Fatalf("WriteFeedbackFiles: %v", err)
	}
	if paths.ScenePath == "" || paths.PreviewPath == "" {
		t.Fatalf("expected both paths to be set, got %+v", paths)
	}

	sceneData, err := os.ReadFile(paths.ScenePath)
	if err != nil {
		t.Fatalf("reading scene file: %v", err)
	}
	var doc struct {
		Type     string         `json:"type"`
		Version  int            `json:"version"`
		Source   string         `json:"source"`
		Elements []any          `json:"elements"`
		AppState map[string]any `json:"appState"`
	}
	if err := json.Unmarshal(sceneData, &doc); err != nil {
		t.Fatalf("unmarshaling .excalidraw doc: %v", err)
	}
	if doc.Type != "excalidraw" || doc.Version != 2 {
		t.Errorf("doc envelope = %+v, want type=excalidraw version=2", doc)
	}
	if len(doc.Elements) != 1 {
		t.Errorf("expected 1 element, got %d", len(doc.Elements))
	}
	if _, ok := doc.AppState["theme"]; ok {
		t.Error("expected theme to be stripped from the published .excalidraw appState")
	}

	previewData, err := os.ReadFile(paths.PreviewPath)
	if err != nil {
		t.Fatalf("reading preview file: %v", err)
	}
	if string(previewData) != string(png) {
		t.Errorf("preview file content = %v, want %v", previewData, png)
	}
}

func TestStore_WriteFeedbackFiles_NoPNGSkipsPreviewFile(t *testing.T) {
	store := forum.NewStore(t.TempDir())
	paths, err := store.WriteFeedbackFiles(testKey, 0, json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("WriteFeedbackFiles: %v", err)
	}
	if paths.PreviewPath != "" {
		t.Errorf("expected empty PreviewPath when no PNG is given, got %q", paths.PreviewPath)
	}
	if _, err := os.Stat(paths.ScenePath); err != nil {
		t.Errorf("expected scene file to exist: %v", err)
	}
}

func TestStore_LayoutMatchesForumsWhiteboardsConvention(t *testing.T) {
	root := t.TempDir()
	store := forum.NewStore(root)
	if err := store.SaveScene(testKey, 3, "h", 0, json.RawMessage(`{}`), nil); err != nil {
		t.Fatalf("SaveScene: %v", err)
	}
	want := filepath.Join(root, "forums", testKey, "whiteboards", "3.json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected scene at %s: %v", want, err)
	}
}
