// Package state names the per-repository directory gate providers keep their
// working files in: an armed loop, an open review.
package state

import (
	"os"
	"path/filepath"

	"github.com/roshbhatia/gate/internal/orc"
)

// Env overrides the directory for every provider at once.
const Env = "GATE_STATE_DIR"

// Dir is the state directory for a working directory. The default is a
// `.gate` folder beside the work; add `.gate/` to the repository's gitignore.
// A bound orc session gets its own subdirectory so two sessions in one
// checkout do not share a ledger.
func Dir(cwd string) string {
	if dir := os.Getenv(Env); dir != "" {
		return dir
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	dir := filepath.Join(cwd, ".gate")
	if session := orc.Session(); session != "" {
		return filepath.Join(dir, "orc", session)
	}
	return dir
}
