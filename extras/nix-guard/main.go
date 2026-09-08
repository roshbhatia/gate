// gate-provider-nix-guard denies an edit whose path resolves into the Nix
// store: the file is generated, and the edit is discarded on the next switch.
package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/roshbhatia/gate/pkg/gate"
)

const storePrefix = "/nix/store/"

// resolve follows symlinks on the path, or on its parent when the file does
// not exist yet. The base name is joined back so a new file under the store
// still carries the store prefix.
func resolve(path string) (string, bool) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, true
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", false
	}
	return filepath.Join(resolved, filepath.Base(path)), true
}

// Decide reports whether the edited path resolves into the store.
func Decide(request gate.Request) gate.Outcome {
	path, _ := request.Event.Input["file_path"].(string)
	if path == "" {
		path, _ = request.Event.Input["notebook_path"].(string)
	}
	if path == "" {
		return gate.PassOutcome()
	}
	resolved, ok := resolve(path)
	if !ok || !strings.HasPrefix(resolved, storePrefix) {
		return gate.PassOutcome()
	}
	return gate.Outcome{
		Kind: gate.Deny,
		Message: fmt.Sprintf("%s resolves to %s, which is Nix-managed and read-only. Edit the Nix source that generates it, then switch. An edit to the store path is discarded on the next switch.",
			path, resolved),
	}
}

func main() {
	gate.Serve(func(request gate.Request) (gate.Outcome, error) { return Decide(request), nil })
}
