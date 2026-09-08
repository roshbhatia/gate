package emit

import (
	"io"

	"github.com/roshbhatia/gate/pkg/gate"
)

// cursorOutput is the union of Cursor's hook responses. Each event reads its
// own fields and ignores the rest, so one shape serves every chain.
type cursorOutput struct {
	Continue          bool           `json:"continue"`
	Permission        string         `json:"permission,omitempty"`
	UserMessage       string         `json:"user_message,omitempty"`
	AgentMessage      string         `json:"agent_message,omitempty"`
	AdditionalContext string         `json:"additional_context,omitempty"`
	FollowupMessage   string         `json:"followup_message,omitempty"`
	UpdatedInput      map[string]any `json:"updated_input,omitempty"`
}

// cursor answers in Cursor's own vocabulary, always exit 0. A deny is
// permission: deny on a tool, continue: false on a prompt, and a follow-up
// message on a stop. A note goes in both agent_message and
// additional_context, since the tool hooks read the first and the post hooks
// read the second.
func cursor(stdout, stderr io.Writer, event string, out gate.Outcome) int {
	response := cursorOutput{Continue: true}
	note := out.Context
	if out.Kind == gate.Context {
		note = out.Message
		if out.Context != "" {
			if note != "" {
				note += "\n\n"
			}
			note += out.Context
		}
	}
	switch out.Kind {
	case gate.Deny, gate.Block:
		reason := out.Message
		if out.Context != "" {
			reason += "\n\n" + out.Context
		}
		switch event {
		case "Stop", "SubagentStop":
			response.FollowupMessage = reason
		case "UserPromptSubmit":
			response.Continue = false
			response.UserMessage = out.Message
			response.AgentMessage = reason
		default:
			response.Permission = "deny"
			response.UserMessage = out.Message
			response.AgentMessage = reason
		}
	case gate.Allow:
		response.Permission = "allow"
		response.UpdatedInput = out.UpdatedInput
		response.AgentMessage = note
		response.AdditionalContext = note
	default:
		response.AgentMessage = note
		response.AdditionalContext = note
	}
	return write(stdout, stderr, response)
}
