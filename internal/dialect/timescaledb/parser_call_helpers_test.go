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

package timescaledb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func applyCallStatement(t *testing.T, sql string) (*ast.CatalogueMutation, error) {
	t.Helper()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NotErrorIs(t, err, fault.ErrPanic)
	return mutation, err
}

func TestRemainingCallArgumentsAreCapturedAsWritten(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		key         string
		wantCapture string
	}{
		{
			name:        "named arguments keep their spelling",
			sql:         `SELECT create_hypertable('readings', 'ts', chunk_time_interval => INTERVAL '1 day', if_not_exists => TRUE)`,
			key:         "TIMESCALE_CALL_EXTRAS",
			wantCapture: `chunk_time_interval => INTERVAL '1 day', if_not_exists => TRUE`,
		},
		{
			name:        "a nested call keeps its parentheses and commas",
			sql:         `SELECT add_retention_policy('conditions', drop_after => make_interval(days => 7, hours => 2))`,
			key:         "TIMESCALE_POLICY_ARGS",
			wantCapture: `drop_after => make_interval(days => 7, hours => 2)`,
		},
		{
			name:        "literals keep their delimiters",
			sql:         `SELECT add_job('proc', '1h', config => E'{"a":\n1}', note => $tag$x$tag$, "Odd" => B'10')`,
			key:         "TIMESCALE_POLICY_ARGS",
			wantCapture: `'1h', config => E'{"a":\n1}', note => $tag$x$tag$, "Odd" => B'10'`,
		},
		{
			name:        "nesting deeper than any capture limit is kept whole",
			sql:         "SELECT create_hypertable('readings', 'ts', x => " + strings.Repeat("(", 80) + "1" + strings.Repeat(")", 80) + ")",
			key:         "TIMESCALE_CALL_EXTRAS",
			wantCapture: "x => " + strings.Repeat("(", 80) + "1" + strings.Repeat(")", 80),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyCallStatement(t, testCase.sql)

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantCapture, mutation.EngineSpecific[testCase.key])
		})
	}
}

func TestMalformedCallsAreLocatedSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantMessage  string
		wantPosition int
	}{
		{
			name:         "an argument that opens a group it never closes",
			sql:          "SELECT create_hypertable('readings', 'ts', x => (1",
			wantMessage:  "unmatched parenthesis",
			wantPosition: len("SELECT create_hypertable('readings', 'ts', x => (1"),
		},
		{
			name:         "a call that is never closed",
			sql:          "SELECT add_retention_policy('conditions', INTERVAL '7 days'",
			wantMessage:  "add_retention_policy: unterminated call",
			wantPosition: len("SELECT add_retention_policy"),
		},
		{
			name:         "a dimension builder that is never closed",
			sql:          "SELECT create_hypertable('readings', by_range('ts', 5",
			wantMessage:  "by_range: unterminated call",
			wantPosition: len("SELECT create_hypertable('readings', by_range"),
		},
		{
			name:         "a call followed by a FROM clause",
			sql:          "SELECT add_retention_policy('conditions', INTERVAL '7 days') FROM hypertables",
			wantMessage:  "expected the end of the statement after the add_retention_policy call",
			wantPosition: len("SELECT add_retention_policy('conditions', INTERVAL '7 days') "),
		},
		{
			name:         "a hypertable call followed by a WHERE clause",
			sql:          "SELECT create_hypertable('readings', 'ts') WHERE false",
			wantMessage:  "expected the end of the statement after the create_hypertable call",
			wantPosition: len("SELECT create_hypertable('readings', 'ts') "),
		},
		{
			name:         "a refresh followed by more tokens",
			sql:          "CALL refresh_continuous_aggregate('hourly', NULL, NULL) extra",
			wantMessage:  "expected the end of the statement after the refresh_continuous_aggregate call",
			wantPosition: len("CALL refresh_continuous_aggregate('hourly', NULL, NULL) "),
		},
		{
			name:         "a refresh with a missing window bound",
			sql:          "CALL refresh_continuous_aggregate('hourly', , NULL)",
			wantMessage:  "expected a window bound",
			wantPosition: len("CALL refresh_continuous_aggregate('hourly', "),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := applyCallStatement(t, testCase.sql)

			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
			assert.Equal(t, testCase.wantPosition, parseError.Position)
		})
	}
}

func TestCapturingCallArgumentsIsLinearInTheirNesting(t *testing.T) {
	t.Parallel()

	statement := func(depth int) string {
		return "SELECT create_hypertable('readings', 'ts', x => " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth) + ")"
	}
	smaller, larger := statement(5_000), statement(20_000)
	engine := New()
	apply := func(sql string) func() {
		return func() {
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			_, err = engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
		}
	}

	scaling.Linear(t, apply(smaller), apply(larger))
}
