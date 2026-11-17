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

//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"fmt"

	"seagull.sh/seagull/cmd/internal/fuzztool/provenance"
)

const (
	// flagRoot names the repository root flag.
	flagRoot = "root"

	// flagFile names the provenance record flag.
	flagFile = "file"
)

// provenanceFlags holds the flags the record, matches and unchanged subcommands share.
type provenanceFlags struct {
	// status is the run's status.
	status string

	// budgetCompleted is true or false.
	budgetCompleted string

	// sourceBefore is the fingerprint taken before the run.
	sourceBefore string

	// engine is the engine identity the run recorded.
	engine string

	// file is the provenance record.
	file string

	// target names the run.
	target provenance.Target
}

// runProvenance runs one provenance subcommand.
//
// Takes ctx (context.Context) which bounds the git and go calls.
// Takes arguments ([]string) which is the subcommand and its flags.
// Takes output (streams) which receives results and diagnostics.
//
// Returns int which is 0, statusFailed for a mismatch or unreadable provenance, or
// statusUsage.
func runProvenance(ctx context.Context, arguments []string, output streams) int {
	if len(arguments) == 0 {
		fmt.Fprintln(output.stderr, "usage: fuzztool provenance fingerprint|record|matches|unchanged ...")
		return statusUsage
	}
	switch arguments[0] {
	case "fingerprint":
		return provenanceFingerprint(ctx, arguments[1:], output)
	case "record", "matches":
		return provenanceCompare(ctx, arguments[0], arguments[1:], output)
	case "unchanged":
		flags := newFlagSet("provenance unchanged", output)
		file := flags.String(flagFile, "", "the provenance record")
		if err := parse(flags, arguments[1:], flagFile); err != nil {
			return statusUsage
		}
		return provenanceStatus(provenance.SourceUnchanged(*file), output)
	default:
		fmt.Fprintf(output.stderr, "fuzztool provenance: unknown subcommand %s\n", arguments[0])
		return statusUsage
	}
}

// provenanceFingerprint prints the source fingerprint.
//
// Takes ctx (context.Context) which bounds the git call.
// Takes arguments ([]string) which holds the flags.
// Takes output (streams) which receives the fingerprint.
//
// Returns int which is the exit status.
func provenanceFingerprint(ctx context.Context, arguments []string, output streams) int {
	flags := newFlagSet("provenance fingerprint", output)
	root := flags.String(flagRoot, "", "the repository root")
	if err := parse(flags, arguments, flagRoot); err != nil {
		return statusUsage
	}
	fingerprint, err := provenance.SourceFingerprint(ctx, *root)
	if err != nil {
		return provenanceStatus(err, output)
	}
	fmt.Fprintln(output.stdout, fingerprint)
	return 0
}

// provenanceCompare writes a record or checks one against the target as it would run.
//
// Takes ctx (context.Context) which bounds the git and go calls.
// Takes action (string) which is record or matches.
// Takes arguments ([]string) which holds the flags.
// Takes output (streams) which receives diagnostics.
//
// Returns int which is the exit status.
func provenanceCompare(ctx context.Context, action string, arguments []string, output streams) int {
	flags := newFlagSet("provenance "+action, output)
	var values provenanceFlags
	flags.StringVar(&values.target.Root, flagRoot, "", "the repository root")
	flags.StringVar(&values.target.Go, "go", "go", "the Go command")
	flags.StringVar(&values.target.Name, "target", "", "the registered target")
	flags.StringVar(&values.target.Module, "module", "", "the module directory")
	flags.StringVar(&values.target.Package, "package", "", "the package path")
	flags.StringVar(&values.target.Function, "function", "", "the fuzz or test function")
	flags.StringVar(&values.target.Command, "command", "", "the campaign action")
	flags.StringVar(&values.target.Budget, "budget", "", "the time and round budget")
	flags.StringVar(&values.target.Workers, "workers", "", "the worker count")
	flags.StringVar(&values.file, flagFile, "", "the provenance record")
	required := []string{flagRoot, "target", "module", "package", "function", "command", "budget", "workers", flagFile}
	if action == "record" {
		flags.StringVar(&values.status, "status", "", "the run's status")
		flags.StringVar(&values.budgetCompleted, "budget-completed", "", "true or false")
		flags.StringVar(&values.sourceBefore, "source-before", "", "the fingerprint taken before the run")
		flags.StringVar(&values.engine, "engine", "", "the engine identity the run recorded")
		required = append(required, "status", "budget-completed", "source-before")
	}
	if err := parse(flags, arguments, required...); err != nil {
		return statusUsage
	}
	if action == "matches" {
		return provenanceStatus(provenance.Matches(ctx, values.target, values.file), output)
	}
	if values.budgetCompleted != "true" && values.budgetCompleted != "false" {
		fmt.Fprintln(output.stderr, "fuzztool provenance record: -budget-completed must be true or false")
		return statusUsage
	}
	outcome := provenance.Outcome{
		Status:          values.status,
		SourceBefore:    values.sourceBefore,
		Engine:          values.engine,
		BudgetCompleted: values.budgetCompleted == "true",
	}
	return provenanceStatus(provenance.Record(ctx, values.target, outcome, values.file), output)
}

// provenanceStatus reports a provenance result as an exit status.
//
// Takes err (error) which is the result.
// Takes output (streams) which receives the reason.
//
// Returns int which is 0 for nil and statusFailed otherwise.
func provenanceStatus(err error, output streams) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, provenance.ErrMismatch) {
		fmt.Fprintln(output.stderr, err)
		return statusFailed
	}

	fmt.Fprintf(output.stderr, "provenance could not be read: %v\n", err)
	return statusFailed
}
