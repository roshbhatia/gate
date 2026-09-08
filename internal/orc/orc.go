// Package orc reads the identity orc binds to a harness session. gate never
// calls orc; it records which session a decision belongs to, so the decision
// log and an orc checkpoint join without orc's bind table.
package orc

import (
	"os"
	"strings"
)

// The environment orc exports into a bound harness.
const (
	SessionEnv = "ORC_SESSION_ID"
	ScopeEnv   = "ORC_SCOPE"
)

// Session is the bound session id, empty when the harness is not bound.
// Callers build paths from it, so an id naming anything but one directory is
// refused rather than escaping into a parent.
func Session() string {
	id := strings.TrimSpace(os.Getenv(SessionEnv))
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return ""
	}
	return id
}

// Scope is the bound scope, empty when the harness is not bound.
func Scope() string { return strings.TrimSpace(os.Getenv(ScopeEnv)) }

// Bound reports whether both halves of the identity are present, which is the
// condition orc's own instructions gate on.
func Bound() bool { return Session() != "" && Scope() != "" }
