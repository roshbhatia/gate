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
}
