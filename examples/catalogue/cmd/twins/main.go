// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

// Command twins checks every cell concepts.yaml marks equivalent: the twin variant is
// copied to a temporary directory, its sgl.yaml is switched to the cell's dialect, and
// the given sgl's generate --check must find the committed code current there. It exits 1
// and lists the cells whose generated code differs:
//
//	go run ./cmd/twins -examples .. -sgl /path/to/sgl
//
// -diff prints what would change for each of them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"seagull.sh/seagull/examples/catalogue"
)

const (
	// statusFailed is the exit status when a cell differs or the check cannot run.
	statusFailed = 1

	// statusUsage is the exit status for a command line that cannot be used.
	statusUsage = 2
)

// main runs the check, stopping it on an interrupt or a termination signal.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	status := run(ctx, os.Args[1:], os.Stdout, os.Stderr, catalogue.RunCommand)
	stop()
	os.Exit(status)
}

// run reads the command line, makes a scratch directory, and runs the twin check.
//
// Takes arguments ([]string) which is the command line without the program name.
// Takes stdout (io.Writer) which receives the report.
// Takes stderr (io.Writer) which receives usage and errors.
// Takes runner (catalogue.CommandRunner) which runs each sgl.
//
// Returns int which is the exit status: 0 when every cell holds, 1 when one differs or
// the check cannot run, 2 for a command line that cannot be used.
func run(ctx context.Context, arguments []string, stdout, stderr io.Writer, runner catalogue.CommandRunner) int {
	flags := flag.NewFlagSet("twins", flag.ContinueOnError)
	flags.SetOutput(stderr)
	examples := flags.String("examples", "..", "the examples directory")
	sgl := flags.String("sgl", "", "the sgl binary to check the twins with")
	diff := flags.Bool("diff", false, "print what would change for each cell that differs")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return statusUsage
	}
	if *sgl == "" {
		_, _ = fmt.Fprintln(stderr, "twins: -sgl is required")
		return statusUsage
	}
	var extra []string
	if *diff {
		extra = append(extra, "--diff")
	}
	scratch, err := os.MkdirTemp("", "sgl-twins-")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "twins:", err)
		return statusFailed
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	results, err := catalogue.CheckTwins(ctx, *examples, scratch, *sgl, extra, runner)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "twins:", err)
		return statusFailed
	}
	return report(stdout, results)
}

// report writes the results and chooses the exit status.
//
// Takes out (io.Writer) which receives the report.
// Takes results ([]catalogue.TwinResult) which are reported.
//
// Returns int which is 0 when every cell holds, 1 otherwise.
func report(out io.Writer, results []catalogue.TwinResult) int {
	var differing []*catalogue.TwinResult
	for index := range results {
		if !results[index].Same {
			differing = append(differing, &results[index])
		}
	}
	if len(differing) == 0 {
		_, _ = fmt.Fprintf(out, "twins: all %d equivalent cells generate their twin's code\n", len(results))
		return 0
	}
	_, _ = fmt.Fprintf(out, "twins: %d of %d equivalent cells generate code that differs from their twin's:\n", len(differing), len(results))
	for _, result := range differing {
		_, _ = fmt.Fprintf(out, "  %s\n", result.Cell)
		for line := range strings.Lines(strings.TrimRight(result.Output, "\n")) {
			_, _ = fmt.Fprintf(out, "      %s", line)
		}
		_, _ = fmt.Fprintln(out)
	}
	return statusFailed
}
