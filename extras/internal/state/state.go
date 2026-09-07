// Package state names the per-repository directory gate providers keep their
// working files in: an armed loop, an open review.
package state

import (
	"os"
	"path/filepath"
)

// Env overrides the directory for every provider at once.
const Env = "GATE_STATE_DIR"

// Dir is the state directory for a working directory. The default is a
// `.gate` folder beside the work, which the owner ignores in git the way
// `.sysinit` already is.
func Dir(cwd string) string {
	if dir := os.Getenv(Env); dir != "" {
		return dir
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return filepath.Join(cwd, ".gate")
}
