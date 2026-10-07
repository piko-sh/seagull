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
	"io"
	"os"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/observe"
)

// generateFlags holds the generate command's flags.
type generateFlags struct {
	// database names one database, or is empty for every one.
	database string

	// check exits 1 when the generated code is out of date, writing nothing.
	check bool

	// diff prints a unified diff of what would change, writing nothing.
	diff bool

	// dryRun lists what would change, writing nothing.
	dryRun bool

	// timings prints how long each stage took.
	timings bool
}

// writes reports whether the command writes files.
//
// Returns bool which is false for --check, --diff and --dry-run.
func (f generateFlags) writes() bool {
	return !f.check && !f.diff && !f.dryRun
}

// runGenerate generates Go code for every selected database.
//
// Takes arguments ([]string) which holds the flags.
//
// Returns int which is the exit status.
func (inv *invocation) runGenerate(ctx context.Context, arguments []string) int {
	flags := inv.newFlags("generate", "generate [--check] [--diff] [--dry-run] [--timings] [--database name]")
	options := generateFlags{database: "", check: false, diff: false, dryRun: false, timings: false}
	flags.StringVar(&options.database, databaseFlag, inv.database, "generate only the `name`d database")
	flags.BoolVar(&options.check, "check", false, "exit 1 when the generated code is out of date; write nothing")
	flags.BoolVar(&options.diff, "diff", false, "print a unified diff of what would change; write nothing")
	flags.BoolVar(&options.dryRun, "dry-run", false, "list what would change; write nothing")
	flags.BoolVar(&options.timings, "timings", false, "print how long each stage took")
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if len(positional) > 0 {
		return inv.fail(usageError("generate takes no arguments; got %q", positional[0]))
	}

	loaded, err := inv.loadProject(options.database)
	if err != nil {
		return inv.fail(err)
	}
	exitStatus := exitOK
	for _, database := range loaded.databases {
		hasFindings, err := inv.generateDatabase(ctx, loaded, database, options)
		if err != nil {
			return inv.fail(err)
		}
		if hasFindings {
			exitStatus = exitFindings
		}
	}
	return exitStatus
}

// generateDatabase generates, compares and writes one database's code.
//
// Takes loaded (*project) which holds the configuration.
// Takes database (*config.Database) which is generated.
// Takes options (generateFlags) which are the command's flags.
//
// Returns bool which is true when the database has error diagnostics or, under --check,
// out-of-date code.
// Returns error when there is a failure that ends the command.
func (inv *invocation) generateDatabase(ctx context.Context, loaded *project, database *config.Database, options generateFlags) (bool, error) {
	var timings *assemble.StageTimer
	sessionSettings := sessionOptions{observer: nil, schemaDSN: "", migrationsOnly: false}
	if options.timings {
		timings = assemble.NewStageTimer(observe.SystemClock())
		sessionSettings.observer = timings
	}
	opened, err := inv.openSession(ctx, loaded, database, sessionSettings)
	if err != nil {
		return false, err
	}
	defer func() { _ = opened.Close() }()

	result, err := opened.generator.Generate(ctx, opened.input)
	if timings != nil {
		writeTimings(inv.streams.Stderr, database.Name, timings.Stages())
	}
	if err != nil {
		return false, fmt.Errorf("database %q: %w", database.Name, err)
	}
	findings := inv.findingsFor(opened.database, loaded, result.Diagnostics)
	if writeErr := writeText(inv.streams.Stderr, findings); writeErr != nil {
		return false, writeErr
	}
	errorCount, _, _ := countFindings(findings)
	if result.Err() != nil && !result.Partial {
		fmt.Fprintf(inv.streams.Stderr, "%s: database %q: %d error(s); no code written\n", inv.program, database.Name, errorCount)
		return true, nil
	}

	headers := append([]string{result.Header}, database.Go.LegacyHeaders...)
	changes, err := planChanges(database.Go.Out, result.Files, headers, options.diff)
	if err != nil {
		return false, err
	}
	if result.Partial {
		return true, inv.applyPartialChanges(database, result, changes, errorCount, options)
	}
	return inv.applyChanges(database, result, changes, options)
}

// applyPartialChanges writes the results for queries that passed analysis.
//
// The run still reports error findings and exits unsuccessfully while writing the usable
// part of the package.
//
// Takes database (*config.Database) which is generated.
// Takes result (*seagull.Result) which holds the partial result.
// Takes changes ([]fileChange) which are the planned changes.
// Takes errorCount (int) which is how many error findings the run has.
// Takes options (generateFlags) which are the command's flags.
//
// Returns error when writing fails.
func (inv *invocation) applyPartialChanges(database *config.Database, result *seagull.Result, changes []fileChange, errorCount int, options generateFlags) error {
	if _, err := inv.applyChanges(database, result, changes, options); err != nil {
		return err
	}
	outcome := "the queries with errors are left out"
	if options.writes() {
		outcome = "the queries with errors were left out and the rest written"
	}
	fmt.Fprintf(inv.streams.Stderr, "%s: database %q: %d error(s); %s\n", inv.program, database.Name, errorCount, outcome)
	return nil
}

// applyChanges reports, diffs or writes a database's planned changes.
//
// Takes database (*config.Database) which is generated.
// Takes result (*seagull.Result) which holds the generated files.
// Takes changes ([]fileChange) which are the planned changes.
// Takes options (generateFlags) which are the command's flags.
//
// Returns bool which is true when --check found out-of-date code.
// Returns error when writing fails.
func (inv *invocation) applyChanges(database *config.Database, result *seagull.Result, changes []fileChange, options generateFlags) (bool, error) {
	if options.diff {
		if err := writeDiff(inv.streams.Stdout, changes, inv.display); err != nil {
			return false, err
		}
	}
	if options.check {
		if len(changes) == 0 {
			fmt.Fprintf(inv.streams.Stdout, "%s: generated code is up to date\n", database.Name)
			return false, nil
		}
		fmt.Fprintf(inv.streams.Stderr, "%[1]s: database %[2]q: generated code in %[3]s is out of date; run `%[1]s generate`\n",
			inv.program, database.Name, inv.display(database.Go.Out))
		inv.listChanges(inv.streams.Stderr, changes)
		return true, nil
	}
	if !options.writes() {
		if options.dryRun {
			inv.listChanges(inv.streams.Stdout, changes)
		}
		return false, nil
	}

	if err := os.MkdirAll(database.Go.Out, directoryPermissions); err != nil {
		return false, fmt.Errorf("creating %s: %w", database.Go.Out, err)
	}
	if err := seagull.WriteFiles(database.Go.Out, result, seagull.WithLegacyHeaders(database.Go.LegacyHeaders...)); err != nil {
		return false, fmt.Errorf("database %q: writing %s: %w", database.Name, database.Go.Out, err)
	}
	fmt.Fprintf(inv.streams.Stdout, "%s: %d queries, %d files in %s (%d changed)\n",
		database.Name, result.QueryCount, len(result.Files), inv.display(database.Go.Out), len(changes))
	return false, nil
}

// listChanges writes one line per planned change.
//
// Takes writer (io.Writer) which receives the list.
// Takes changes ([]fileChange) which are listed.
func (inv *invocation) listChanges(writer io.Writer, changes []fileChange) {
	for _, change := range changes {
		fmt.Fprintf(writer, "  %-6s %s\n", change.kind, inv.display(change.path))
	}
}
