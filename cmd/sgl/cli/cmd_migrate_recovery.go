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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"piko.sh/safeconv"

	"seagull.sh/seagull/sdk/migrate"
)

const (
	// statementIndent indents the lines of a statement printed under its number.
	statementIndent = "       "

	// maxAnswerBytes is the longest line sgl reads as the answer to a prompt.
	maxAnswerBytes = 1 << 10
)

// migrateForce records a migration as applied without running it, after confirmation.
//
// Takes migrator (*migrate.Migrator) which rewrites the history.
// Takes version (int64) which is the migration to force.
// Takes options (*migrateFlags) which holds --yes.
//
// Returns int which is the exit status.
func (inv *invocation) migrateForce(ctx context.Context, migrator *migrate.Migrator, version int64, options *migrateFlags) int {
	statuses, err := migrator.Status(ctx)
	if err != nil {
		return inv.fail(migrationFailure(err))
	}
	name, state := "", ""
	for _, status := range statuses {
		if status.Version == version {
			name, state = status.Name, migrationState(status)
		}
	}
	if name == "" {
		return inv.fail(migrationFailure(&migrate.UnknownMigrationError{Version: version}))
	}
	fmt.Fprintf(inv.streams.Stderr, "Migration %d (%s) is %s. %s will record it as applied, with its files' checksums, without running it.\n",
		version, name, state, inv.program)
	if confirmed, err := inv.confirm(options, "Force it"); err != nil || !confirmed {
		return inv.declined(err)
	}
	if err := migrator.Force(ctx, version); err != nil {
		return inv.fail(migrationFailure(err))
	}
	fmt.Fprintf(inv.streams.Stdout, "forced %d %s\n", version, name)
	return exitOK
}

// migrateRepair re-baselines the checksums of changed migrations and resolves dirty ones,
// after showing what it will change and asking how much of each dirty migration ran.
//
// Takes migrator (*migrate.Migrator) which rewrites the history.
// Takes arguments (migrateArguments) which holds the optional version.
// Takes options (*migrateFlags) which holds --yes and --completed.
//
// Returns int which is the exit status.
func (inv *invocation) migrateRepair(ctx context.Context, migrator *migrate.Migrator, arguments migrateArguments, options *migrateFlags) int {
	var versions []int64
	if arguments.hasVersion {
		versions = append(versions, arguments.version)
	}
	plan, err := migrator.PlanRepair(ctx, versions...)
	if err != nil {
		return inv.fail(migrationFailure(err))
	}
	inv.writeMissing(plan.Missing)
	if plan.Empty() {
		if options.completed != -1 {
			return inv.fail(usageError("--completed: no migration is dirty"))
		}
		fmt.Fprintln(inv.streams.Stdout, "nothing to repair")
		return exitOK
	}
	inv.writeRepairPlan(plan)
	completed, err := inv.resolutions(plan, options)
	if err != nil {
		return inv.fail(err)
	}
	if confirmed, err := inv.confirm(options, "Repair the history"); err != nil || !confirmed {
		return inv.declined(err)
	}
	result, err := migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: completed})
	if err != nil {
		return inv.fail(migrationFailure(err))
	}
	inv.writeRepairResult(plan, completed, result)
	return exitOK
}

// resolutions decides how many statements of each dirty migration ran: --completed for
// the one dirty migration, or the answer to a prompt for each.
//
// Takes plan (*migrate.RepairPlan) which lists the dirty migrations.
// Takes options (*migrateFlags) which holds --yes and --completed.
//
// Returns map[int64]int which maps each migration to resolve to its completed statements.
// Returns error which is a usage error when the dirty migrations cannot be resolved as
// asked, or the failure to read an answer.
func (inv *invocation) resolutions(plan *migrate.RepairPlan, options *migrateFlags) (map[int64]int, error) {
	completed := map[int64]int{}
	if options.completed != -1 {
		if len(plan.Dirty) != 1 {
			return nil, usageError("--completed needs exactly one dirty migration, and %d are dirty; name one with a version", len(plan.Dirty))
		}
		dirty := plan.Dirty[0]
		if options.completed > len(dirty.Statements) {
			return nil, usageError("--completed: migration %d has %d statements, not %d", dirty.Version, len(dirty.Statements), options.completed)
		}
		completed[dirty.Version] = options.completed
		return completed, nil
	}
	for _, dirty := range plan.Dirty {
		if !inv.interactive() {
			return nil, usageError("migration %d is dirty; check which of its statements ran, then pass --completed with how many did", dirty.Version)
		}
		count, resolve, err := inv.askCompleted(dirty)
		if err != nil {
			return nil, err
		}
		if resolve {
			completed[dirty.Version] = count
		}
	}
	return completed, nil
}

// askCompleted asks how many of a dirty migration's statements ran, until the answer is a
// count within them or empty.
//
// Takes dirty (migrate.DirtyMigration) which is the migration.
//
// Returns int which is how many statements ran.
// Returns bool which is false when the answer is empty, to leave the migration dirty.
// Returns error when no answer can be read.
func (inv *invocation) askCompleted(dirty migrate.DirtyMigration) (int, bool, error) {
	total := len(dirty.Statements)
	for {
		answer, err := inv.ask(fmt.Sprintf("How many of migration %d's %d statements ran? Enter 0 to %d, or nothing to leave it dirty: ",
			dirty.Version, total, total))
		if err != nil || answer == "" {
			return 0, false, err
		}
		count, parseErr := strconv.Atoi(answer)
		if parseErr == nil && count >= 0 && count <= total {
			return count, true, nil
		}
		fmt.Fprintf(inv.streams.Stderr, "%q is not a number from 0 to %d.\n", answer, total)
	}
}

// writeRepairPlan describes what a repair will change.
//
// Takes plan (*migrate.RepairPlan) which is described.
func (inv *invocation) writeRepairPlan(plan *migrate.RepairPlan) {
	writer := inv.streams.Stderr
	for _, changed := range plan.Changed {
		fmt.Fprintf(writer, "Migration %d (%s) changed since it was applied:\n", changed.Version, changed.Name)
		if changed.RecordedChecksum != changed.FileChecksum {
			fmt.Fprintf(writer, "  up file:   recorded %s, now %s\n", changed.RecordedChecksum, changed.FileChecksum)
		}
		if changed.RecordedDownChecksum != "" && changed.RecordedDownChecksum != changed.DownFileChecksum {
			fmt.Fprintf(writer, "  down file: recorded %s, now %s\n", changed.RecordedDownChecksum, emptyAsNone(changed.DownFileChecksum))
		}
		fmt.Fprintf(writer, "  %s will record the files' checksums. The migration is not run again, so keep only edits that change nothing it did.\n", inv.program)
	}
	for _, dirty := range plan.Dirty {
		fmt.Fprintf(writer, "Migration %d (%s) is dirty: %s.\n", dirty.Version, dirty.Name, recordedProgress(dirty))
		if !dirty.ChecksumMatch {
			fmt.Fprintln(writer, "  Its file changed since the run; the statements below are the file as it is now.")
		}
		for index, statement := range dirty.Statements {
			fmt.Fprintf(writer, "  %3d. %s\n", index+1, strings.ReplaceAll(statement, "\n", "\n"+statementIndent))
		}
	}
}

// writeMissing warns about applied migrations whose files are gone, which repair cannot
// change.
//
// Takes missing ([]int64) which holds their versions.
func (inv *invocation) writeMissing(missing []int64) {
	for _, version := range missing {
		fmt.Fprintf(inv.streams.Stderr, "Migration %d is applied but has no file; restore the file, as repair cannot change it.\n", version)
	}
}

// writeRepairResult prints what a repair changed.
//
// Takes plan (*migrate.RepairPlan) which names the migrations.
// Takes completed (map[int64]int) which holds the resolutions.
// Takes result (migrate.RepairResult) which lists what changed.
func (inv *invocation) writeRepairResult(plan *migrate.RepairPlan, completed map[int64]int, result migrate.RepairResult) {
	for _, changed := range plan.Changed {
		fmt.Fprintf(inv.streams.Stdout, "re-baselined %d %s\n", changed.Version, changed.Name)
	}
	for _, dirty := range plan.Dirty {
		count, resolved := completed[dirty.Version]
		switch {
		case !resolved:
			fmt.Fprintf(inv.streams.Stdout, "left %d %s dirty\n", dirty.Version, dirty.Name)
		case count == 0:
			fmt.Fprintf(inv.streams.Stdout, "reset %d %s: pending, it runs from its first statement\n", dirty.Version, dirty.Name)
		case count == len(dirty.Statements):
			fmt.Fprintf(inv.streams.Stdout, "resolved %d %s: applied\n", dirty.Version, dirty.Name)
		default:
			fmt.Fprintf(inv.streams.Stdout, "resolved %d %s: up resumes at statement %d of %d\n", dirty.Version, dirty.Name, count+1, len(dirty.Statements))
		}
	}
	fmt.Fprintf(inv.streams.Stdout, "%d re-baselined, %d resolved\n", len(result.Rebaselined), len(result.Resolved))
}

// interactive reports whether standard input is a terminal, which is when sgl asks for
// confirmation. A pipe, a file or another device, such as /dev/zero, is not.
//
// Returns bool which is true when standard input is a terminal.
func (inv *invocation) interactive() bool {
	file, isFile := inv.streams.Stdin.(interface{ Fd() uintptr })
	if !isFile {
		return false
	}
	return inv.terminal(safeconv.Uint64ToInt(uint64(file.Fd())))
}

// confirm asks a yes or no question on a terminal, unless --yes answered it.
//
// Takes options (*migrateFlags) which holds --yes.
// Takes question (string) which is asked, without the question mark.
//
// Returns bool which is true when the answer is yes.
// Returns error when no answer can be read.
func (inv *invocation) confirm(options *migrateFlags, question string) (bool, error) {
	if options.yes {
		return true, nil
	}
	answer, err := inv.ask(question + "? [y/N] ")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// declined ends a command whose confirmation was refused or could not be read.
//
// Takes err (error) which is the failure to read the answer, or nil when it was no.
//
// Returns int which is 1 when the answer was no, or the failure's status.
func (inv *invocation) declined(err error) int {
	if err != nil {
		return inv.fail(err)
	}
	fmt.Fprintf(inv.streams.Stderr, "%s: nothing changed\n", inv.program)
	return exitFindings
}

// ask prints a prompt on standard error and reads one line of standard input, of at most
// maxAnswerBytes bytes.
//
// Takes prompt (string) which is printed.
//
// Returns string which is the line without surrounding space.
// Returns error which is a usage error when standard input ends before an answer or the
// line is longer than maxAnswerBytes.
func (inv *invocation) ask(prompt string) (string, error) {
	if inv.input == nil {
		inv.input = bufio.NewReaderSize(inv.streams.Stdin, maxAnswerBytes)
	}
	_, _ = io.WriteString(inv.streams.Stderr, prompt)
	line, err := inv.input.ReadSlice('\n')
	switch {
	case errors.Is(err, bufio.ErrBufferFull):
		return "", usageError("the answer is longer than %d bytes", maxAnswerBytes)
	case errors.Is(err, io.EOF) && len(line) == 0:
		return "", usageError("no answer: standard input ended")
	case err != nil && !errors.Is(err, io.EOF):
		return "", err
	default:
		return strings.TrimSpace(string(line)), nil
	}
}

// recordedProgress describes the progress the history records for a dirty migration.
//
// Takes dirty (migrate.DirtyMigration) which is the migration.
//
// Returns string which says how many statements the history records as run.
func recordedProgress(dirty migrate.DirtyMigration) string {
	if dirty.LastStatement < 0 {
		return "the history records none of its statements as run"
	}
	return fmt.Sprintf("the history records its first %d statements as run, counted as the interrupted run divided the file", dirty.LastStatement+1)
}

// emptyAsNone prints an empty checksum as none.
//
// Takes checksum (string) which may be empty.
//
// Returns string which is the checksum or "none".
func emptyAsNone(checksum string) string {
	if checksum == "" {
		return "none"
	}
	return checksum
}
