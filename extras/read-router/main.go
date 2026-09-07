// gate-provider-read-router denies an unbounded Read of a large file and names
// the two alternatives: a ranged Read, or the cheap reader.
package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/roshbhatia/gate/extras/internal/bulkread"
	"github.com/roshbhatia/gate/pkg/gate"
)

// Read handles these by page or by pixel, so a line limit means nothing on them.
var opaqueToLineLimits = map[string]bool{
	".pdf": true, ".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".webp": true, ".bmp": true, ".ipynb": true,
}

// Decide is the whole decision, with no harness in it.
func Decide(request gate.Request) gate.Outcome {
	path, _ := request.Event.Input["file_path"].(string)
	if path == "" {
		return gate.PassOutcome()
	}
	// An explicit range is the caller having already decided what it needs.
	if _, ok := request.Event.Input["offset"]; ok {
		return gate.PassOutcome()
	}
	if _, ok := request.Event.Input["limit"]; ok {
		return gate.PassOutcome()
	}
	if opaqueToLineLimits[strings.ToLower(filepath.Ext(path))] {
		return gate.PassOutcome()
	}
	if !filepath.IsAbs(path) && request.Event.Cwd != "" {
		path = filepath.Join(request.Event.Cwd, path)
	}
	options := bulkread.FromRequest(request)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= options.TriggerBytes {
		return gate.PassOutcome()
	}
	return bulkread.Redirect(options, path, info.Size())
}

func main() {
	gate.Serve(func(request gate.Request) (gate.Outcome, error) { return Decide(request), nil })
}
