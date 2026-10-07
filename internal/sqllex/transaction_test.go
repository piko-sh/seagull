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

package sqllex_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqllex"
)

func vacuumRules() sqllex.Rules {
	return withRules(func(rules *sqllex.Rules) {
		rules.Quotes.DollarQuoting = true
		rules.RequiresNoTransaction = func(tokens []sqllex.Token) bool {
			return len(tokens) > 0 && tokens[0].IsWord("VACUUM")
		}
	})
}

func TestRequiresNoTransaction(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		err      error
		name     string
		input    string
		rules    sqllex.Rules
		expected bool
	}{
		{name: "a matching statement", rules: vacuumRules(), input: "CREATE TABLE t (id int);\n-- note\nvacuum t;", expected: true},
		{name: "no matching statement", rules: vacuumRules(), input: "CREATE TABLE t (id int); SELECT 'vacuum';", expected: false},
		{name: "a mention in a comment", rules: vacuumRules(), input: "/* VACUUM */ SELECT 1;", expected: false},
		{name: "a mention in a dollar-quoted body", rules: vacuumRules(), input: "DO $$\nVACUUM;\n$$;", expected: false},
		{name: "a quoted identifier", rules: vacuumRules(), input: `"vacuum";`, expected: false},
		{name: "no predicate", rules: standardRules, input: "VACUUM;", expected: false},
		{name: "a script that cannot be split", rules: vacuumRules(), input: "VACUUM; SELECT 'open", expected: false, err: sqllex.ErrUnterminatedString},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			required, err := sqllex.RequiresNoTransaction(testCase.rules, testCase.input)
			if testCase.err != nil {
				require.ErrorIs(t, err, testCase.err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.expected, required)
		})
	}
}

func TestRequiresNoTransactionReturnsCallbackPanicAsError(t *testing.T) {
	t.Parallel()

	errVacuum := errors.New("vacuum rejected")

	testCases := []struct {
		cause    error
		name     string
		input    string
		callback string
		message  string
		rules    sqllex.Rules
	}{
		{
			name: "predicate panics with a string",
			rules: withRules(func(rules *sqllex.Rules) {
				rules.RequiresNoTransaction = func([]sqllex.Token) bool { panic("boom") }
			}),
			input:    "SELECT 1;\nVACUUM t;",
			callback: "RequiresNoTransaction",
			message:  "checking the statement at line 1, column 1 (byte 0): lexical rules callback RequiresNoTransaction panicked: boom",
		},
		{
			name: "predicate panics with an error on a later statement",
			rules: withRules(func(rules *sqllex.Rules) {
				rules.RequiresNoTransaction = func(tokens []sqllex.Token) bool {
					if tokens[0].IsWord("VACUUM") {
						panic(errVacuum)
					}
					return false
				}
			}),
			input:    "SELECT 1;\n  VACUUM t;\nVACUUM u;",
			callback: "RequiresNoTransaction",
			cause:    errVacuum,
			message:  "checking the statement at line 2, column 3 (byte 12): lexical rules callback RequiresNoTransaction panicked: vacuum rejected",
		},
		{
			name: "procedural block callback panics while splitting",
			rules: withRules(func(rules *sqllex.Rules) {
				rules.ProceduralBlock = func([]string) bool { panic("boom") }
				rules.RequiresNoTransaction = func([]sqllex.Token) bool { return true }
			}),
			input:    "CREATE TRIGGER t BEGIN SELECT 1; END;",
			callback: "ProceduralBlock",
			message:  "checking BEGIN at line 1, column 18 (byte 17): lexical rules callback ProceduralBlock panicked: boom",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			required, err := sqllex.RequiresNoTransaction(testCase.rules, testCase.input)
			require.False(t, required)
			require.ErrorIs(t, err, sqllex.ErrRulesCallback)
			require.NotErrorIs(t, err, sqllex.ErrMalformed)
			require.Equal(t, testCase.message, err.Error())
			var callbackError *sqllex.CallbackError
			require.ErrorAs(t, err, &callbackError)
			require.Equal(t, testCase.callback, callbackError.Callback)
			if testCase.cause != nil {
				require.ErrorIs(t, err, testCase.cause)
			}
		})
	}
}

func TestSignificantTokens(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{name: "skips noise", input: " a /* b */ c -- d\n", expected: []string{"a", "c"}},
		{name: "stops at the first error", input: "a 'open", expected: []string{"a"}},
		{name: "empty", input: "", expected: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var raws []string
			for _, token := range sqllex.SignificantTokens(standardRules, testCase.input) {
				raws = append(raws, token.Raw)
			}
			require.Equal(t, testCase.expected, raws)
		})
	}
}
