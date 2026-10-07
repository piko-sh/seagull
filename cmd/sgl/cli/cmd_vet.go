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

package cli

import (
	"context"
	"fmt"
)

// runVet analyses every selected database and reports problems without writing anything.
//
// Takes arguments ([]string) which holds the flags.
//
// Returns int which is the exit status: 1 when there are errors, or warnings under
// --strict.
func (inv *invocation) runVet(ctx context.Context, arguments []string) int {
	flags := inv.newFlags("vet", "vet [--strict] [--format text|json|github|sarif] [--database name]")
	database := flags.String(databaseFlag, inv.database, "vet only the `name`d database")
	strict := flags.Bool("strict", false, "treat warnings as findings")
	format := flags.String("format", formatText, "output format: text, json, github or sarif")
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if len(positional) > 0 {
		return inv.fail(usageError("vet takes no arguments; got %q", positional[0]))
	}
	switch *format {
	case formatText, formatJSON, formatGitHub, formatSARIF:
	default:
		return inv.fail(usageError("unknown --format %q: want text, json, github or sarif", *format))
	}

	loaded, err := inv.loadProject(*database)
	if err != nil {
		return inv.fail(err)
	}
	var findings []finding
	for _, selected := range loaded.databases {
		opened, err := inv.openSession(ctx, loaded, selected, sessionOptions{observer: nil, schemaDSN: "", migrationsOnly: false})
		if err != nil {
			return inv.fail(err)
		}
		report, err := opened.generator.Vet(ctx, opened.input)
		_ = opened.Close()
		if err != nil {
			return inv.fail(fmt.Errorf("database %q: %w", selected.Name, err))
		}
		findings = append(findings, inv.findingsFor(opened.database, loaded, report.Diagnostics)...)
	}

	if err := writeFindings(inv.streams.Stdout, *format, findings); err != nil {
		return inv.fail(err)
	}
	errorCount, warningCount, hintCount := countFindings(findings)
	if *format == formatText {
		fmt.Fprintf(inv.streams.Stderr, "%d error(s), %d warning(s), %d hint(s)\n", errorCount, warningCount, hintCount)
	}
	if errorCount > 0 || (*strict && warningCount > 0) {
		return exitFindings
	}
	return exitOK
}
