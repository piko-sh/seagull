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

// Command export writes the examples manifest the website reads, and copies every
// variant's files beside it:
//
//	go run ./cmd/export -examples .. -out /tmp/examples
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
	// statusFailed is the exit status when the export fails.
	statusFailed = 1

	// statusUsage is the exit status for a command line that cannot be used.
	statusUsage = 2
)

// main runs the export, stopping it on an interrupt or a termination signal.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	status := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(status)
}

// run reads the command line and exports the examples.
//
// Takes arguments ([]string) which is the command line without the program name.
// Takes stdout (io.Writer) which receives the summary.
// Takes stderr (io.Writer) which receives usage and errors.
//
// Returns int which is the exit status: 0 when the export is written, 1 when it fails, 2
// for a command line that cannot be used.
func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	examples := flags.String("examples", "..", "the examples directory")
	out := flags.String("out", "", "the directory to write manifest.json and the files into")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return statusUsage
	}
	if *out == "" {
		_, _ = fmt.Fprintln(stderr, "export: -out is required")
		return statusUsage
	}
	manifest, err := catalogue.Export(ctx, *examples, *out)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "export:", err)
		return statusFailed
	}
	_, _ = fmt.Fprintf(stdout, "exported %d concepts and %d variants to %s\n", len(manifest.Concepts), len(manifest.Variants), *out)
	return 0
}
