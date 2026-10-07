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
	"errors"
	"fmt"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/migrate"
)

// commandHint returns the command that resolves a library error, for the line the CLI
// prints under the error, or empty when there is none. The library's messages name the
// operation, since a program that embeds it has no command line; only the CLI knows its
// own commands.
//
// Takes err (error) which is the failure.
// Takes program (string) which is the name the commands start with.
//
// Returns string which is the hint, or empty.
func commandHint(err error, program string) string {
	later, isLater := errors.AsType[*migrate.LaterMigrationError](err)
	mismatch, isMismatch := errors.AsType[*migrate.ResumeMismatchError](err)
	execution, isExecution := errors.AsType[*migrate.MigrationExecutionError](err)
	dirty, isDirty := errors.AsType[*migrate.DirtyMigrationError](err)
	switch {
	case errors.Is(err, migrate.ErrDirtyRollback) && isDirty:
		return fmt.Sprintf("finish migration %[2]d with `%[1]s migrate up`, or record how many of its statements ran with `%[1]s migrate repair %[2]d`, then roll back",
			program, dirty.Version)
	case errors.Is(err, migrate.ErrLockLost):
		return fmt.Sprintf("run `%s migrate status` to see where the run stopped, and check the server's and any pooler's idle timeouts before running again", program)
	case isLater && later.LaterDirty:
		return fmt.Sprintf("run `%s migrate repair %d` first", program, later.Later)
	case isLater:
		return fmt.Sprintf("run `%[1]s migrate down` first, or `%[1]s migrate force %[2]d` instead", program, later.Later)
	case isMismatch:
		return fmt.Sprintf("once you know how many of its statements ran, run `%s migrate repair %d`", program, mismatch.Version)
	case errors.Is(err, context.Canceled) && isExecution:
		return fmt.Sprintf("run `%[1]s migrate status`; if migration %[2]d is dirty, `%[1]s migrate repair %[2]d` resolves it", program, execution.Version)
	default:
		return ""
	}
}

// diagnosticHint returns the command that resolves a diagnostic, given as its help when
// the diagnostic has none of its own, or empty when there is none.
//
// Takes diagnostic (seagull.SourceError) which is the diagnostic.
// Takes program (string) which is the name the command starts with.
//
// Returns string which is the hint, or empty.
func diagnosticHint(diagnostic seagull.SourceError, program string) string {
	if diagnostic.Code == seagull.CodeForeignPrefix {
		return fmt.Sprintf("once the prefix is accepted, `%s fmt -w` rewrites it to the canonical prefix", program)
	}
	return ""
}
