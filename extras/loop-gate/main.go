// gate-provider-loop-gate holds a Stop until a declared command passes.
//
// The model arms it with the command that decides the phase is done; the Stop
// hook then runs that command and blocks the turn while it fails, up to a cap
// and a stall bound. This is the deterministic loop every large shop runs
// (Spotify's Honk, Dropbox's Nova): the thing that iterates is a check, and the
// model only gets to stop when the check agrees.
//
// The same binary is the CLI: `loop-gate arm --until '<command>' [--max n]
// [--stall n]`, `status`, and `clear`. With `decide` or no argument it serves
// the gate.decide action.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/roshbhatia/gate/extras/internal/state"
	"github.com/roshbhatia/gate/internal/orc"
	"github.com/roshbhatia/gate/pkg/gate"
)

const usage = "usage: loop-gate arm --until '<command>' [--max n] [--stall n] | status | clear | decide"

type loop struct {
	Until     string `json:"until"`
	Max       int    `json:"max"`
	Stall     int    `json:"stall"`
	Iter      int    `json:"iter"`
	SameCount int    `json:"sameCount"`
	LastHash  string `json:"lastHash"`
}

func stateFile(cwd string) string { return filepath.Join(state.Dir(cwd), "loop-gate.json") }

func read(path string) (loop, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return loop{}, false
	}
	var s loop
	if json.Unmarshal(data, &s) != nil {
		return loop{}, false
	}
	return s, true
}

func write(path string, s loop) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func arm(args []string) int {
	s := loop{Max: 4, Stall: 2}
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			fmt.Fprintf(os.Stderr, "loop-gate: %s needs a value\n", args[i])
			return 1
		}
		value := args[i+1]
		var err error
		switch args[i] {
		case "--until":
			s.Until = value
		case "--max":
			s.Max, err = strconv.Atoi(value)
		case "--stall":
			s.Stall, err = strconv.Atoi(value)
		default:
			fmt.Fprintf(os.Stderr, "loop-gate: unknown flag: %s\n", args[i])
			return 1
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "loop-gate: --max and --stall must be integers")
			return 1
		}
		i++
	}
	if s.Until == "" {
		fmt.Fprintln(os.Stderr, "loop-gate: arm requires --until '<command>'")
		return 1
	}
	if err := write(stateFile(""), s); err != nil {
		fmt.Fprintf(os.Stderr, "loop-gate: %s\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "loop-gate: armed. STOP is `%s`; CAPPED at %d, STALLED after %d unchanged.\n", s.Until, s.Max, s.Stall)
	return 0
}

func status() int {
	path := stateFile("")
	s, ok := read(path)
	if !ok {
		fmt.Printf("loop-gate: disarmed (no state at %s)\n", path)
		return 0
	}
	fmt.Printf("loop-gate: armed\n  STOP:    %s\n  iter:    %d/%d\n  unchanged: %d/%d\n", s.Until, s.Iter, s.Max, s.SameCount, s.Stall)
	return 0
}

func clear() int {
	if err := os.Remove(stateFile("")); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "loop-gate: %s\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "loop-gate: disarmed.")
	return 0
}

func shell() string {
	if path, err := exec.LookPath("bash"); err == nil {
		return path
	}
	return "sh"
}

func run(command, dir string) (string, int) {
	cmd := exec.Command(shell(), "-c", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	text := strings.TrimRight(string(out), "\n")
	if err == nil {
		return text, 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return text, exit.ExitCode()
	}
	return text, 1
}

// Decide advances the loop by one iteration. The state is removed on a pass,
// a stall, or a cap, and written back otherwise. A cap or a stall is reported
// as open work in Context, never as a pass in silence.
func Decide(request gate.Request) gate.Outcome {
	path := stateFile(request.Event.Cwd)
	s, ok := read(path)
	if !ok {
		return gate.PassOutcome()
	}
	out, code := run(s.Until, request.Event.Cwd)
	if code == 0 {
		_ = os.Remove(path)
		return gate.Outcome{Kind: gate.Context, Message: fmt.Sprintf("loop-gate: CLEAN after %d iteration(s); `%s` exited 0.", s.Iter, s.Until)}
	}
	s.Iter++
	sum := sha256.Sum256([]byte(out))
	hash := hex.EncodeToString(sum[:])
	if hash == s.LastHash {
		s.SameCount++
	} else {
		s.SameCount = 0
	}
	s.LastHash = hash
	if s.SameCount >= s.Stall {
		_ = os.Remove(path)
		return gate.Outcome{Kind: gate.Context, Message: fmt.Sprintf("loop-gate: STALLED; %d iterations produced identical output from `%s`. Open work, not a pass.", s.SameCount, s.Until)}
	}
	if s.Iter >= s.Max {
		_ = os.Remove(path)
		return gate.Outcome{Kind: gate.Context, Message: fmt.Sprintf("loop-gate: CAPPED at %d iterations; `%s` still failing. Open work, not a pass.", s.Max, s.Until)}
	}
	if err := write(path, s); err != nil {
		return gate.Outcome{Kind: gate.Context, Message: "loop-gate: " + err.Error()}
	}
	if request.Event.StopActive {
		return gate.PassOutcome()
	}
	return gate.Outcome{
		Kind: gate.Block,
		Message: fmt.Sprintf(`The declared STOP condition is not met (iteration %d/%d).

Command: %s
Exit code: %d

Output:
%s

Fix the cause and continue. Do not report this phase as done while the command fails.%s`,
			s.Iter, s.Max, s.Until, code, out, orcNote()),
	}
}

// orcNote answers a bound orc session. A blocked Stop is not the final
// response, so a checkpoint written now reports one milestone twice.
func orcNote() string {
	if !orc.Bound() {
		return ""
	}
	return "\nThis turn continues, so it is not the final response. Write the orc checkpoint once, after the command exits 0."
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "decide" {
		gate.Serve(func(request gate.Request) (gate.Outcome, error) { return Decide(request), nil })
		return
	}
	switch args[0] {
	case "arm":
		os.Exit(arm(args[1:]))
	case "status":
		os.Exit(status())
	case "clear":
		os.Exit(clear())
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}
