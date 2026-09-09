package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/roshbhatia/gate/pkg/gate"
)

func Decide(gate.Request) (gate.Outcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "note", "context")
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return gate.Outcome{}, fmt.Errorf("note context: %w: %s", err, stderr.String())
	}
	if len(output) == 0 {
		return gate.PassOutcome(), nil
	}
	return gate.Outcome{Kind: gate.Context, Message: string(output)}, nil
}
func main() { gate.Serve(Decide) }
