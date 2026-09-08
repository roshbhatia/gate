package state

import (
	"path/filepath"
	"testing"
)

func TestDir(t *testing.T) {
	for _, tc := range []struct{ name, env, want string }{
		{"default", "", filepath.Join("/repo", ".gate")},
		{"absolute override wins", "/tmp/s", "/tmp/s"},
		{"relative override rides the event cwd", ".gate/s1", filepath.Join("/repo", ".gate", "s1")},
		{"relative override is cleaned", "./.gate/bound/s1/", filepath.Join("/repo", ".gate", "bound", "s1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(Env, tc.env)
			if got := Dir("/repo"); got != tc.want {
				t.Fatalf("Dir = %q, want %q", got, tc.want)
			}
		})
	}
}
