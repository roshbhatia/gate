package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/roshbhatia/go-utils/provider"
)

func frame(t *testing.T, capability string, in Request) string {
	t.Helper()
	input, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	request := provider.Request{
		Version: provider.Version, Kind: provider.FrameRequest, RequestID: "r1",
		Capability: capability, Input: input,
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

func TestRunRoundTripsADecision(t *testing.T) {
	in := Request{
		Event: Envelope{Version: Version, Harness: "claude", Event: "PreToolUse", Tool: "Read",
			Input: map[string]any{"file_path": "/tmp/x"}},
		Args: map[string]any{"trigger_kib": float64(16), "names": []any{"a", "b"}},
	}
	var out bytes.Buffer
	err := Run(func(r Request) (Outcome, error) {
		if r.Event.Tool != "Read" || r.Int("trigger_kib", 0) != 16 || len(r.Strings("names")) != 2 {
			t.Fatalf("request did not survive the trip: %+v", r)
		}
		return Outcome{Kind: Deny, Message: "no", Context: "note"}, nil
	}, strings.NewReader(frame(t, Action, in)), &out)
	if err != nil {
		t.Fatal(err)
	}
	var result provider.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != provider.ResultOK || result.RequestID != "r1" {
		t.Fatalf("result = %+v", result)
	}
	var outcome Outcome
	if err := json.Unmarshal(result.Output, &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != Deny || outcome.Message != "no" || outcome.Context != "note" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestRunReportsErrorsAndRejectsOtherCapabilities(t *testing.T) {
	var out bytes.Buffer
	err := Run(func(Request) (Outcome, error) { return Outcome{}, errors.New("boom") },
		strings.NewReader(frame(t, Action, Request{})), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"status":"error"`) || !strings.Contains(out.String(), "boom") {
		t.Fatalf("error result = %s", out.String())
	}
	if err := Run(func(Request) (Outcome, error) { return PassOutcome(), nil },
		strings.NewReader(frame(t, "inference.generate", Request{})), &out); err == nil {
		t.Fatal("a foreign capability was accepted")
	}
}
