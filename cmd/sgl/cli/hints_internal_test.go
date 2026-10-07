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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/migrate"
)

func TestCommandHintNamesTheCommandThatResolvesALibraryError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
		want string
	}{
		{
			name: "a later dirty migration is repaired",
			err:  fmt.Errorf("forcing: %w", &migrate.LaterMigrationError{Version: 2, Later: 3, LaterDirty: true}),
			want: "run `sgl migrate repair 3` first",
		},
		{
			name: "a later applied migration is rolled back or forced instead",
			err:  &migrate.LaterMigrationError{Version: 1, Later: 2, LaterDirty: false},
			want: "run `sgl migrate down` first, or `sgl migrate force 2` instead",
		},
		{
			name: "a migration whose progress no longer matches is repaired",
			err:  &migrate.ResumeMismatchError{Version: 7, Name: "boats", LastStatement: 1, RecordedSplitterVersion: 1, SplitterVersion: 2, StatementCount: 3},
			want: "once you know how many of its statements ran, run `sgl migrate repair 7`",
		},
		{
			name: "an interrupted migration is checked and repaired",
			err:  fmt.Errorf("applying: %w", &migrate.MigrationExecutionError{Cause: context.Canceled, Name: "boats", Version: 5, Direction: migrate.MigrationDirectionUp}),
			want: "run `sgl migrate status`; if migration 5 is dirty, `sgl migrate repair 5` resolves it",
		},
		{
			name: "a rollback refused over a dirty migration finishes or repairs it first",
			err:  fmt.Errorf("rolling back: %w: %w", migrate.ErrDirtyRollback, &migrate.DirtyMigrationError{Version: 4, LastStatement: 1}),
			want: "finish migration 4 with `sgl migrate up`, or record how many of its statements ran with `sgl migrate repair 4`, then roll back",
		},
		{
			name: "a lost lock is checked before running again",
			err:  fmt.Errorf("applying migration 3: %w", migrate.ErrLockLost),
			want: "run `sgl migrate status` to see where the run stopped, and check the server's and any pooler's idle timeouts before running again",
		},
		{
			name: "a migration that failed for another reason has no hint",
			err:  &migrate.MigrationExecutionError{Cause: errors.New("syntax error"), Name: "boats", Version: 5, Direction: migrate.MigrationDirectionUp},
			want: "",
		},
		{
			name: "any other error has no hint",
			err:  migrate.ErrLockNotAcquired,
			want: "",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, commandHint(testCase.err, defaultProgram))
			require.Equal(t, strings.ReplaceAll(testCase.want, "`sgl ", "`seagull "), commandHint(testCase.err, "seagull"))
		})
	}
}

func TestDiagnosticHintNamesTheFormatterForAForeignPrefix(t *testing.T) {
	t.Parallel()

	require.Equal(t, "once the prefix is accepted, `sgl fmt -w` rewrites it to the canonical prefix",
		diagnosticHint(seagull.SourceError{Code: seagull.CodeForeignPrefix}, defaultProgram))
	require.Equal(t, "once the prefix is accepted, `seagull fmt -w` rewrites it to the canonical prefix",
		diagnosticHint(seagull.SourceError{Code: seagull.CodeForeignPrefix}, "seagull"))
	require.Empty(t, diagnosticHint(seagull.SourceError{Code: seagull.CodeAliasPrefix}, defaultProgram))
}
