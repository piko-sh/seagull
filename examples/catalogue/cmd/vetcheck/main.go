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

// Command vetcheck checks every variant's expected_diagnostics: it runs the given sgl's
// vet --format json in each variant directory that holds an sgl.yaml, and the codes it
// reports must be the codes example.yaml lists, no more and no fewer. It exits 1 and
// lists every variant that differs, or that sgl could not vet:
//
//	go run ./cmd/vetcheck -examples .. -sgl /path/to/sgl
//
// A few variants cannot be vetted standalone and are skipped, each with its reason; -v
// also lists every variant that matched.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"seagull.sh/seagull/examples/catalogue"
)

const (
	// statusFailed is the exit status when a variant differs or the check cannot run.
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

// run reads the command line, makes a scratch directory, and runs the check.
//
// Takes arguments ([]string) which is the command line without the program name.
// Takes stdout (io.Writer) which receives the report.
// Takes stderr (io.Writer) which receives usage and errors.
// Takes runner (catalogue.CommandRunner) which runs each sgl.
//
// Returns int which is the exit status: 0 when every vetted variant matches, 1 when one
// differs or the check cannot run, 2 for a command line that cannot be used.
func run(ctx context.Context, arguments []string, stdout, stderr io.Writer, runner catalogue.CommandRunner) int {
	flags := flag.NewFlagSet("vetcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	examples := flags.String("examples", "..", "the examples directory")
	sgl := flags.String("sgl", "", "the sgl binary to vet the variants with")
	goTool := flags.String("go", "go", "the go command, for the variants that are their own sgl")
	verbose := flags.Bool("v", false, "list every variant, not only the ones that differ")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return statusUsage
	}
	if *sgl == "" {
		_, _ = fmt.Fprintln(stderr, "vetcheck: -sgl is required")
		return statusUsage
	}
	scratch, err := os.MkdirTemp("", "sgl-vetcheck-")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "vetcheck:", err)
		return statusFailed
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	results, err := catalogue.CheckDiagnostics(ctx, *examples, scratch, *sgl, *goTool, runner)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "vetcheck:", err)
		return statusFailed
	}
	return report(stdout, results, *verbose)
}

// report writes the results and chooses the exit status.
//
// Takes out (io.Writer) which receives the report.
// Takes results ([]catalogue.VetResult) which are reported.
// Takes verbose (bool) which lists the variants that matched too.
//
// Returns int which is 0 when no vetted variant differs, 1 otherwise.
func report(out io.Writer, results []catalogue.VetResult, verbose bool) int {
	var skipped, differing []*catalogue.VetResult
	for index := range results {
		result := &results[index]
		switch {
		case result.Skipped != "":
			skipped = append(skipped, result)
		case result.Differs():
			differing = append(differing, result)
		case verbose:
			_, _ = fmt.Fprintf(out, "  %s\n", result)
		}
	}
	vetted := len(results) - len(skipped)
	for _, result := range skipped {
		_, _ = fmt.Fprintf(out, "vetcheck: %s\n", result)
	}
	if len(differing) == 0 {
		_, _ = fmt.Fprintf(out, "vetcheck: all %d vetted variants report their expected_diagnostics\n", vetted)
		return 0
	}
	_, _ = fmt.Fprintf(out, "vetcheck: %d of %d vetted variants differ from their expected_diagnostics:\n", len(differing), vetted)
	for _, result := range differing {
		_, _ = fmt.Fprintf(out, "  %s\n", result)
	}
	return statusFailed
}
