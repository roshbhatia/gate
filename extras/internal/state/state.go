// Package state names the per-repository directory gate providers keep their
// working files in: an armed loop, an open review.
package state

import (
	"os"
	"path/filepath"
)

// Env overrides the directory. An absolute value is used as given. A relative
// one is resolved against the event's working directory, which is how a
// caller scopes state per session without knowing where the hook will run:
// export GATE_STATE_DIR=.gate/<whatever names the session>.
const Env = "GATE_STATE_DIR"

// Dir is the state directory for a working directory. The default is a
// `.gate` folder beside the work; add `.gate/` to the repository's gitignore.
func Dir(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	dir := os.Getenv(Env)
	if dir == "" {
		return filepath.Join(cwd, ".gate")
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(cwd, dir)
}
