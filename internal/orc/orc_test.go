package orc

import "testing"

func TestSessionRefusesAnythingButOneSegment(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"abc", "abc"},
		{"  abc  ", "abc"},
		{"", ""},
		{".", ""},
		{"..", ""},
		{"a/b", ""},
		{`a\b`, ""},
	} {
		t.Setenv(SessionEnv, tc.id)
		if got := Session(); got != tc.want {
			t.Errorf("Session(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestBoundNeedsBothHalves(t *testing.T) {
	for _, tc := range []struct {
		session, scope string
		want           bool
	}{
		{"abc", "repo", true},
		{"abc", "", false},
		{"", "repo", false},
		{"..", "repo", false},
	} {
		t.Setenv(SessionEnv, tc.session)
		t.Setenv(ScopeEnv, tc.scope)
		if got := Bound(); got != tc.want {
			t.Errorf("Bound(%q, %q) = %v, want %v", tc.session, tc.scope, got, tc.want)
		}
	}
}
