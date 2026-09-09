package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/roshbhatia/gate/pkg/gate"
)

func TestAdapter(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		want         gate.Kind
		fail         bool
	}{
		{"empty", "exit 0", gate.Pass, false},
		{"context", "printf 'Review token validation'", gate.Context, false},
		{"failure", "echo corrupt >&2; exit 1", gate.Pass, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir)
			if err := os.WriteFile(filepath.Join(dir, "note"), []byte("#!/bin/sh\n[ \"$1\" = context ] || exit 2\n"+tc.script), 0700); err != nil {
				t.Fatal(err)
			}
			out, err := Decide(gate.Request{})
			if (err != nil) != tc.fail {
				t.Fatalf("error: %v", err)
			}
			if !tc.fail && out.Kind != tc.want {
				t.Fatalf("outcome: %+v", out)
			}
		})
	}
}
