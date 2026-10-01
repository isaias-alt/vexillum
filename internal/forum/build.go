package forum

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"runtime/debug"
	"sync"
)

var buildID = sync.OnceValue(func() string {
	h := sha256.New()
	// The whiteboard frame bundle is large but its postMessage protocol has to
	// match whiteboard-embed.js, so a stale server serving an old bundle breaks
	// the whiteboard silently; the fonts and css never do, so skip those.
	for _, root := range []string{"assets/chrome", "assets/whiteboard-embed.js", "assets/whiteboard/whiteboard.js.gz", "assets/favicon.svg"} {
		_ = fs.WalkDir(assetsFS, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			data, readErr := fs.ReadFile(assetsFS, path)
			if readErr == nil {
				h.Write([]byte(path))
				h.Write(data)
			}
			return nil
		})
	}
	id := hex.EncodeToString(h.Sum(nil))[:16]
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				id += "-" + setting.Value
			}
		}
	}
	return id
})

// Build identifies this binary's forum assets and revision. The forum server
// is one long-lived process per user, shared by every vexillum invocation, so
// a server started by an older binary can outlive an upgrade (an open browser
// tab keeps it alive); comparing builds lets the newer binary replace it
// instead of silently serving the old chrome.
func Build() string { return buildID() }
