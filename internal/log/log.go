// Package log appends one JSON line per hook call: what each provider decided
// and what the harness was told. A reader joins a line to its own records by
// the harness session, or by whatever `log_fields` was told to carry.
package log

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/roshbhatia/gate/pkg/gate"
)

// Decision is one provider's answer inside a call.
type Decision struct {
	Provider string    `json:"provider"`
	Kind     gate.Kind `json:"kind"`
	Ms       int64     `json:"ms"`
	Message  string    `json:"message,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Record is one hook call.
type Record struct {
	Ts      time.Time `json:"ts"`
	Harness string    `json:"harness"`
	Event   string    `json:"event"`
	Session string    `json:"session,omitempty"`
	// Fields carries whatever identity the config asked for. gate does not
	// know what a field means; a neighbouring tool declares its own so a
	// decision joins that tool's records without either side importing the
	// other.
	Fields    map[string]string `json:"fields,omitempty"`
	Agent     string            `json:"agent,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	Tool      string            `json:"tool,omitempty"`
	Final     gate.Kind         `json:"final"`
	Ms        int64             `json:"ms"`
	Decisions []Decision        `json:"decisions,omitempty"`
}

// messageLimit keeps a record to one line of reasonable width; the full
// message went to the harness already.
const messageLimit = 512

// Clip bounds a message for the record.
func Clip(text string) string {
	if len(text) <= messageLimit {
		return text
	}
	return text[:messageLimit] + "…"
}

// Append writes the record. A missing directory is created; a write failure
// is returned rather than fatal, because the decision already reached the
// harness and the log is not what a hook is for.
func Append(path string, record Record) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode log record: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer file.Close()
	_, err = file.Write(append(encoded, '\n'))
	return err
}
