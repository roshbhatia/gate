package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/roshbhatia/gate/pkg/gate"
)

func TestDecideOnlyDeniesStorePaths(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.nix")
	if err := os.WriteFile(plain, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := gate.Request{Event: gate.Envelope{Input: map[string]any{"file_path": plain}}}
	if out := Decide(req); out.Kind != gate.Pass {
		t.Fatalf("plain file decision = %+v", out)
	}
	if out := Decide(gate.Request{Event: gate.Envelope{Input: map[string]any{}}}); out.Kind != gate.Pass {
		t.Fatalf("no path decision = %+v", out)
	}
	if _, err := os.Stat("/nix/store"); err == nil {
		req.Event.Input["file_path"] = "/nix/store/anything"
		if out := Decide(req); out.Kind != gate.Deny {
			t.Fatalf("store decision = %+v", out)
		}
	}
	// A new file under an existing directory resolves through its parent and
	// keeps its own name, so a store child that does not exist yet is caught.
	// TempDir is itself behind a symlink on macOS, where /var resolves to
	// /private/var, so the expectation has to resolve too.
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, ok := resolve(filepath.Join(dir, "new.nix")); !ok || resolved != filepath.Join(resolvedDir, "new.nix") {
		t.Fatalf("resolve of a new file = %q, %v", resolved, ok)
	}
}
