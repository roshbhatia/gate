// Package bulkread is the rule two providers share: a whole-file read of a
// large file is denied, and the denial names the cheap reader.
package bulkread

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/roshbhatia/gate/pkg/gate"
)

// DefaultTriggerKiB is the size above which a whole-file read is routed.
// Spotify measured the break-even for handing a read to a cheap model at about
// 350 lines, which is what 16 KiB of source is; below it the round trip costs
// more than it saves.
const DefaultTriggerKiB = 16

// DefaultReader is the command the denial names. It is spelled out in full
// until ask carries a per-provider light model, at which point the template
// alone selects it.
const DefaultReader = "ask -p claude -m haiku -t bulk-read"

// Options come from the chain step's args.
type Options struct {
	TriggerBytes int64
	Reader       string
}

// FromRequest reads the shared options out of a provider request.
func FromRequest(request gate.Request) Options {
	return Options{
		TriggerBytes: int64(request.Int("trigger_kib", DefaultTriggerKiB)) * 1024,
		Reader:       request.String("reader", DefaultReader),
	}
}

// sampleBytes bounds what EstimateLines reads. Counting the whole file would
// pay the cost the rule exists to prevent.
const sampleBytes = 64 * 1024

// EstimateLines scales the newline density of the first sample over the size.
func EstimateLines(path string, size int64) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	buf := make([]byte, sampleBytes)
	n, _ := io.ReadFull(f, buf)
	if n == 0 {
		return 0
	}
	newlines := strings.Count(string(buf[:n]), "\n")
	if newlines == 0 {
		return 1
	}
	return int(float64(newlines) * float64(size) / float64(n))
}

// Redirect is the deny a whole-file read of a large file gets. A deny is the
// one PreToolUse decision whose reason the model reads, so the alternatives
// live in it: the ranged read for an edit, the cheap reader for an answer.
func Redirect(options Options, path string, size int64) gate.Outcome {
	return gate.Outcome{
		Kind: gate.Deny,
		Message: fmt.Sprintf(`%s is %d KiB (about %d lines). A whole-file read is blocked.
  - Need a range: Grep for it, then Read with offset and limit.
  - Need the shape or an answer: %s --var question='<what you need>' < %s
    It returns bullets only, each leading with a name or a line; the file never enters your context.`,
			filepath.Base(path), size/1024, EstimateLines(path, size), options.Reader, path),
	}
}
