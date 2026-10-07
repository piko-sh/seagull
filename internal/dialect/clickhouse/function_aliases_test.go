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

package clickhouse

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestFunctionAliasTableDecodesEveryLine(t *testing.T) {
	t.Parallel()

	aliases, err := functionAliasTable()

	require.NoError(t, err)
	require.NotEmpty(t, aliases)
	require.Equal(t, aliases, functionAliases())
}

func TestParseFunctionAliasesReportsEveryLineThatDoesNotDecode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		source      string
		wantAliases map[string]string
		wantErrors  []error
		wantText    []string
	}{
		{
			name:        "aliases filed in lower case, with comments and blank lines ignored",
			source:      "# comment\n\nDATE_DIFF dateDiff\n  replace replaceAll  \n",
			wantAliases: map[string]string{"date_diff": "datediff", "replace": "replaceall"},
		},
		{
			name:        "a line with too many names",
			source:      "DATE_DIFF dateDiff\nbroken line here\nreplace replaceAll\n",
			wantAliases: map[string]string{"date_diff": "datediff", "replace": "replaceall"},
			wantErrors:  []error{errMalformedAliasLine},
			wantText:    []string{"line 2", "got 3 words"},
		},
		{
			name:        "a line with one name",
			source:      "lonely\nreplace replaceAll\n",
			wantAliases: map[string]string{"replace": "replaceall"},
			wantErrors:  []error{errMalformedAliasLine},
			wantText:    []string{"line 1", "got 1 words"},
		},
		{
			name:        "an alias listed again for another function",
			source:      "replace replaceAll\nREPLACE replaceOne\n",
			wantAliases: map[string]string{"replace": "replaceall"},
			wantErrors:  []error{errConflictingAlias},
			wantText:    []string{"line 2", `"REPLACE"`},
		},
		{
			name:        "an alias listed again in another case for the same function",
			source:      "DATE_DIFF dateDiff\ndate_diff dateDiff\n",
			wantAliases: map[string]string{"date_diff": "datediff"},
		},
		{
			name:        "a line too long to read",
			source:      "replace replaceAll\n" + strings.Repeat("x", bufio.MaxScanTokenSize+1) + " y\nlater alias\n",
			wantAliases: map[string]string{"replace": "replaceall"},
			wantErrors:  []error{bufio.ErrTooLong},
			wantText:    []string{"after line 1"},
		},
		{
			name:        "several problems are all reported",
			source:      "one\nreplace replaceAll\nreplace again\nthree names here\n",
			wantAliases: map[string]string{"replace": "replaceall"},
			wantErrors:  []error{errMalformedAliasLine, errConflictingAlias},
			wantText:    []string{"line 1", "line 3", "line 4"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			aliases, err := parseFunctionAliases(testCase.source)
			require.Equal(t, testCase.wantAliases, aliases)
			if len(testCase.wantErrors) == 0 {
				require.NoError(t, err)
				return
			}
			for _, want := range testCase.wantErrors {
				require.ErrorIs(t, err, want)
			}
			for _, text := range testCase.wantText {
				require.Contains(t, err.Error(), text)
			}
		})
	}
}

func TestFunctionAliasesTakeTheSignaturesOfTheFunctionTheyName(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	testCases := []struct {
		name   string
		alias  string
		target string
	}{
		{name: "an SQL-standard spelling", alias: "replace", target: "replaceall"},
		{name: "a MySQL spelling", alias: "date_diff", target: "datediff"},
		{name: "an upper-case spelling", alias: "year", target: "toyear"},
		{name: "an aggregate", alias: "group_concat", target: "groupconcat"},
		{name: "a function this round added", alias: "from_unixtime", target: "fromunixtimestamp"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, functions[testCase.target])
			require.Equal(t, functions[testCase.target], functions[testCase.alias])
		})
	}
}

func TestFunctionAliasesLeaveUnknownFunctionsAndRegisteredNamesAlone(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions

	_, fullHostName := functions["fullhostname"]
	require.False(t, fullHostName, "the alias of a function the catalogue lacks stays unknown")
	require.Equal(t, "median", functions["median"][0].Name, "an alias the catalogue holds keeps its own signatures")
}

func TestResolverReadsAnAliasAsTheFunctionItNames(t *testing.T) {
	t.Parallel()

	uint32Type := primitiveType("UInt32")
	resolver := newCatalogueFunctionResolver(New().BuiltinFunctions())
	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
		want      sqlmodel.SQLType
	}{
		{name: "an alias of an identity aggregate", function: "medianExact", arguments: []sqlmodel.SQLType{uint32Type}, want: uint32Type},
		{name: "an alias with a combinator", function: "medianExactIf", arguments: []sqlmodel.SQLType{uint32Type, primitiveType("UInt8")}, want: uint32Type},
		{name: "an upper-case alias", function: "BIT_AND", arguments: []sqlmodel.SQLType{uint32Type}, want: uint32Type},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.arguments)
			require.NoError(t, err)
			require.Equal(t, testCase.want, resolution.ReturnType)
		})
	}
}
