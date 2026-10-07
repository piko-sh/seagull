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

package diagbuild_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqllex"
)

func splitError(t *testing.T, text string) error {
	t.Helper()

	_, err := sqllex.Split(sqllex.Rules{}, text)
	require.Error(t, err)
	return fmt.Errorf("splitting SQL: %w", err)
}

func TestFailurePlacesTheErrorWhereTheDialectSaysItIs(t *testing.T) {
	t.Parallel()

	text := "-- sgl.query(name: Get, command: one)\nSELECT id FROM items ) ;\n"
	unterminated := "-- sgl.query(name: Get, command: one)\nSELECT id\nFROM items /* open\n"
	testCases := []struct {
		err  error
		want diag.SourceError
		text string
		name string
	}{
		{
			name: "a parse error at its token",
			text: text,
			err:  &dialectkit.ParseError{Token: ")", Message: "expected the end of the statement", Position: 59},
			want: diag.SourceError{Filename: "queries/get.sql", Line: 11, Column: 22, EndLine: 11, EndColumn: 23, Code: diag.CodeParseError,
				Severity: diag.SeverityError, Message: `failed to parse query "Get": syntax error near ")": expected the end of the statement`},
		},
		{
			name: "a wrapped parse error at the end of the text",
			text: text,
			err:  fmt.Errorf("tokenising SQL: %w", &dialectkit.ParseError{Token: "", Message: "expected FROM", Position: len(text)}),
			want: diag.SourceError{Filename: "queries/get.sql", Line: 12, Column: 1, EndLine: 12, EndColumn: 1, Code: diag.CodeParseError,
				Severity: diag.SeverityError, Message: `failed to parse query "Get": tokenising SQL: syntax error at end of input: expected FROM`},
		},
		{
			name: "a parse error whose token is not where it says",
			text: text,
			err:  &dialectkit.ParseError{Token: "WHERE", Message: "unexpected", Position: 59},
			want: diag.SourceError{Filename: "queries/get.sql", Line: 10, Column: 1, Code: diag.CodeParseError,
				Severity: diag.SeverityError, Message: `failed to parse query "Get": syntax error near "WHERE": unexpected`},
		},
		{
			name: "a lexical error, at the rest of its line and without its position",
			text: unterminated,
			err:  splitError(t, unterminated),
			want: diag.SourceError{Filename: "queries/get.sql", Line: 12, Column: 12, EndLine: 12, EndColumn: 19, Code: diag.CodeParseError,
				Severity: diag.SeverityError, Message: `failed to parse query "Get": splitting SQL: unterminated block comment near "/* open\n"`},
		},
		{
			name: "an error with no place",
			text: text,
			err:  errors.New("unsupported statement"),
			want: diag.SourceError{Filename: "queries/get.sql", Line: 10, Column: 1, Code: diag.CodeParseError,
				Severity: diag.SeverityError, Message: `failed to parse query "Get": unsupported statement`},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, diagbuild.Failure("queries/get.sql", testCase.text, 10, `failed to parse query "Get"`, testCase.err))
		})
	}
}

func panickingSplitError(t *testing.T, text string) error {
	t.Helper()

	rules := sqllex.Rules{ProceduralBlock: func([]string) bool { panic("procedural block rule broke") }}
	_, err := sqllex.Split(rules, text)
	require.ErrorIs(t, err, sqllex.ErrRulesCallback)
	return err
}

func TestFailureReportsARecoveredPanicAsQ070(t *testing.T) {
	t.Parallel()

	text := "-- sgl.query(name: Get, command: one)\nSELECT id FROM items ) ;\n"
	procedure := "CREATE PROCEDURE touch() BEGIN SELECT 1; END;\n"
	splitPanic := panickingSplitError(t, procedure)
	testCases := []struct {
		err  error
		want diag.SourceError
		text string
		name string
	}{
		{
			name: "a panic the dialect recovered",
			text: text,
			err:  fmt.Errorf("dialect postgres: %w: %v", fault.ErrPanic, "index out of range"),
			want: diag.SourceError{Filename: "queries/get.sql", Line: 10, Column: 1, Code: diag.CodeRecoveredPanic,
				Severity: diag.SeverityError, Message: `failed to parse query "Get": dialect postgres: recovered a panic: index out of range`},
		},
		{
			name: "a recovered panic that still says where it happened",
			text: text,
			err:  errors.Join(fault.ErrPanic, &dialectkit.ParseError{Token: ")", Message: "expected the end of the statement", Position: 59}),
			want: diag.SourceError{Filename: "queries/get.sql", Line: 11, Column: 22, EndLine: 11, EndColumn: 23, Code: diag.CodeRecoveredPanic,
				Severity: diag.SeverityError,
				Message:  "failed to parse query \"Get\": recovered a panic\nsyntax error near \")\": expected the end of the statement"},
		},
		{
			name: "a panic in a lexical rules callback while splitting",
			text: procedure,
			err:  splitPanic,
			want: diag.SourceError{Filename: "queries/get.sql", Line: 10, Column: 1, Code: diag.CodeRecoveredPanic,
				Severity: diag.SeverityError, Message: `failed to parse query "Get": ` + splitPanic.Error()},
		},
		{
			name: "a lexical rules callback that panicked with an error",
			text: text,
			err:  fmt.Errorf("checking the statement: %w", &sqllex.CallbackError{Value: errors.New("rule table missing"), Callback: "RequiresNoTransaction"}),
			want: diag.SourceError{Filename: "queries/get.sql", Line: 10, Column: 1, Code: diag.CodeRecoveredPanic,
				Severity: diag.SeverityError,
				Message:  `failed to parse query "Get": checking the statement: lexical rules callback RequiresNoTransaction panicked: rule table missing`},
		},
		{
			name: "an error that only mentions a panic stays a parse error",
			text: text,
			err:  errors.New("recovered a panic"),
			want: diag.SourceError{Filename: "queries/get.sql", Line: 10, Column: 1, Code: diag.CodeParseError,
				Severity: diag.SeverityError, Message: `failed to parse query "Get": recovered a panic`},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, diagbuild.Failure("queries/get.sql", testCase.text, 10, `failed to parse query "Get"`, testCase.err))
		})
	}
}

func TestFailureWithoutAnErrorReportsTheContext(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		startLine int
		context   string
	}{
		{name: "a query", startLine: 4, context: `failed to parse query "Get"`},
		{name: "a migration statement", startLine: 1, context: "failed to apply statement"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var failure diag.SourceError
			require.NotPanics(t, func() { failure = diagbuild.Failure("q.sql", "SELECT 1;", testCase.startLine, testCase.context, nil) })
			require.Equal(t, diag.CodeParseError, failure.Code)
			require.Equal(t, diag.SeverityError, failure.Severity)
			require.Equal(t, testCase.context, failure.Message)
			require.Equal(t, testCase.startLine, failure.Line)
			require.Equal(t, "q.sql", failure.Filename)
		})
	}
}
