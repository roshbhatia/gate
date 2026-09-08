// Package state names the per-repository directory gate providers keep their
// working files in: an armed loop, an open review.
package state

import (
	"os"
	"path/filepath"
	"strings"
)

// Env overrides the directory for every provider at once.
const Env = "GATE_STATE_DIR"

// SessionEnv names the orc session the harness is bound to. orc binds several
// sessions to one checkout, and an armed loop or an open review belongs to the
// session that armed it, not to the repository.
const SessionEnv = "ORC_SESSION_ID"

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
	if session := safeSegment(os.Getenv(SessionEnv)); session != "" {
		return filepath.Join(dir, "orc", session)
	}
	return dir
}

// safeSegment keeps a session id that names one directory. An id carrying a
// separator or a dot entry would escape the state directory.
func safeSegment(id string) string {
	if id == "" || id == "." || id == ".." {
		return ""
	}
	if strings.ContainsAny(id, `/\`) {
		return ""
	}
	return id
}
