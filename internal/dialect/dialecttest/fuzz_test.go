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

package dialecttest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/martian"
	"seagull.sh/seagull/internal/testkit/scaling"
)

const (
	stalledInputDeadline = 50 * time.Millisecond
	stalledInputBudget   = 2 * time.Second
)

type stallingDialect struct {
	*martian.Dialect
}

func (stallingDialect) ParseStatements(ctx context.Context, _ string) ([]ast.ParsedStatement, error) {
	<-ctx.Done()
	return nil, context.Cause(ctx)
}

func TestGenerateInvariantProblemsStopsAnInputAtItsDeadline(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	testCases := []struct {
		ctx         context.Context
		engine      dialect.DialectPort
		name        string
		migration   string
		query       string
		expectError []error
	}{
		{
			name:        "an input that generates keeps every invariant",
			ctx:         t.Context(),
			engine:      martian.New(),
			migration:   martianGoldenMigration,
			query:       martianUnknownColumnQuery,
			expectError: nil,
		},
		{
			name:        "an input whose SQL is not valid still keeps every invariant",
			ctx:         t.Context(),
			engine:      martian.New(),
			migration:   "CREATE TABLE items (id WHOLE64 PRIMARY KEY, name TEXTUAL DEFAULT 'x);",
			query:       "SELECT || FROM",
			expectError: nil,
		},
		{
			name:        "a dialect that stalls until its context is done fails the input at its deadline",
			ctx:         t.Context(),
			engine:      stallingDialect{Dialect: martian.New()},
			migration:   martianGoldenMigration,
			query:       martianGoldenQuery,
			expectError: []error{errFuzzInputDeadline},
		},
		{
			name:        "generation failing with neither a limit nor malformed SQL fails the input",
			ctx:         cancelled,
			engine:      martian.New(),
			migration:   martianGoldenMigration,
			query:       martianGoldenQuery,
			expectError: []error{errUnexpectedGenerateError, context.Canceled},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problems, err := generateInvariantProblems(testCase.ctx, testCase.engine, testCase.migration, testCase.query, stalledInputDeadline)
			require.Empty(t, problems)
			if len(testCase.expectError) == 0 {
				require.NoError(t, err)
				return
			}
			for _, expected := range testCase.expectError {
				require.ErrorIs(t, err, expected)
			}
		})
	}
}

func TestGenerateInvariantProblemsStopsAStalledInputWellBeforeTheWatchdog(t *testing.T) {
	t.Parallel()

	scaling.Within(t, stalledInputBudget, func(ctx context.Context) {
		_, err := generateInvariantProblems(ctx, stallingDialect{Dialect: martian.New()}, martianGoldenMigration, martianGoldenQuery,
			stalledInputDeadline)
		require.ErrorIs(t, err, errFuzzInputDeadline)
	})
}

func TestGenerateInvariantProblemsRefusesADialectTheGeneratorCannotUse(t *testing.T) {
	t.Parallel()

	problems, err := generateInvariantProblems(t.Context(), nil, martianGoldenMigration, martianGoldenQuery, stalledInputDeadline)
	require.Empty(t, problems)
	require.ErrorContains(t, err, "creating the generator: ")
}

func FuzzGenerateSeedsFromGoldenCases(f *testing.F) {
	directory := f.TempDir()
	for name, content := range map[string]string{
		"001_case/" + specFile:                       martianGoldenSpec,
		"001_case/migrations/001_items.up.sql":       martianGoldenMigration,
		"001_case/migrations/001_items.down.sql":     "DROP TABLE items;",
		"001_case/migrations/notes.txt":              "not SQL",
		"001_case/queries/get_item.sql":              martianGoldenQuery,
		"001_case/queries/unknown_column.sql":        martianUnknownColumnQuery,
		"002_migration_only/" + specFile:             martianGoldenSpec,
		"002_migration_only/migrations/001_a.up.sql": martianGoldenMigration,
		"loose_file.txt":                             "",
	} {
		writeFuzzCaseFile(f, directory, name, content)
	}
	FuzzGenerate(f, newMartian, directory)
}
