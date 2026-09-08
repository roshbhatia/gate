package state

import (
	"path/filepath"
	"testing"

	"github.com/roshbhatia/gate/internal/orc"
)

func TestDir(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"default", nil, filepath.Join("/repo", ".gate")},
		{"override wins", map[string]string{Env: "/tmp/s"}, "/tmp/s"},
		{"orc session scopes", map[string]string{orc.SessionEnv: "abc"}, filepath.Join("/repo", ".gate", "orc", "abc")},
		{"override beats session", map[string]string{Env: "/tmp/s", orc.SessionEnv: "abc"}, "/tmp/s"},
		{"traversal is refused", map[string]string{orc.SessionEnv: "../../etc"}, filepath.Join("/repo", ".gate")},
		{"dot entry is refused", map[string]string{orc.SessionEnv: ".."}, filepath.Join("/repo", ".gate")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The suite may itself run inside a bound orc session.
			t.Setenv(Env, "")
			t.Setenv(orc.SessionEnv, "")
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := Dir("/repo"); got != tc.want {
				t.Fatalf("Dir = %q, want %q", got, tc.want)
			}
		})
	}
}
