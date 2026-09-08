// Package event normalizes a harness hook payload into a gate.Envelope.
package event

import (
	"encoding/json"
	"fmt"

	"github.com/roshbhatia/gate/pkg/gate"
)

// Known lists the hook events a chain may be configured for. The names are
// Claude Code's; codex and gemini use the same ones and cursor's are mapped
// in normalizeCursor.
var Known = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"SubagentStart", "SubagentStop", "Stop", "SessionEnd", "Notification", "PreCompact",
}

// harnessPayload is the Claude Code hook input. Codex writes the same shape and
// gemini's agy wraps it unchanged, so one decoder serves the three.
type harnessPayload struct {
	HookEventName        string          `json:"hook_event_name"`
	SessionID            string          `json:"session_id"`
	Cwd                  string          `json:"cwd"`
	ToolName             string          `json:"tool_name"`
	ToolInput            map[string]any  `json:"tool_input"`
	ToolResponse         json.RawMessage `json:"tool_response"`
	AgentID              string          `json:"agent_id"`
	AgentType            string          `json:"agent_type"`
	StopHookActive       bool            `json:"stop_hook_active"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	Prompt               string          `json:"prompt"`
	Source               string          `json:"source"`
}

// Normalize decodes one hook payload. The event argument wins over the
// payload's own name when both are present, because the chain was selected by
// the flag before the payload was read.
func Normalize(harness, event string, raw []byte) (gate.Envelope, error) {
	if harness == "json" {
		var env gate.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return env, fmt.Errorf("decode gate envelope: %w", err)
		}
		if env.Version == "" {
			env.Version = gate.Version
		}
		if event != "" {
			env.Event = event
		}
		return env, nil
	}
	if harness == "cursor" {
		return normalizeCursor(event, raw)
	}
	var payload harnessPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return gate.Envelope{}, fmt.Errorf("decode %s hook payload: %w", harness, err)
		}
	}
	if event == "" {
		event = payload.HookEventName
	}
	if event == "" {
		return gate.Envelope{}, fmt.Errorf("the %s payload names no event and --event was not given", harness)
	}
	return gate.Envelope{
		Version:     gate.Version,
		Harness:     harness,
		Event:       event,
		Tool:        payload.ToolName,
		Input:       payload.ToolInput,
		Response:    payload.ToolResponse,
		Session:     payload.SessionID,
		Agent:       gate.Agent{ID: payload.AgentID, Type: payload.AgentType},
		Cwd:         payload.Cwd,
		StopActive:  payload.StopHookActive,
		LastMessage: payload.LastAssistantMessage,
		Prompt:      payload.Prompt,
		Source:      payload.Source,
		Raw:         json.RawMessage(raw),
	}, nil
}
