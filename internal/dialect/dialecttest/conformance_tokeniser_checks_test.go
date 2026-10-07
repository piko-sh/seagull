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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres"
)

type oneStatementDialect struct {
	dialect.DialectPort
}

func (oneStatementDialect) ParseStatements(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
	return []ast.ParsedStatement{{Raw: nil, Location: 0, Length: len(sql)}}, nil
}

func wordsAndStrings(source string) ([]Token, error) {
	var tokens []Token
	for position := 0; position < len(source); {
		switch source[position] {
		case ' ':
			position++
		case '\'':
			end := strings.IndexByte(source[position+1:], '\'')
			tokens = append(tokens, Token{Position: position, Class: TokenOther})
			position += end + 2
		default:
			end := strings.IndexAny(source[position:], " ;")
			if end == 0 {
				tokens = append(tokens, Token{Position: position, Class: TokenOther})
				position++
				continue
			}
			if end < 0 {
				end = len(source) - position
			}
			tokens = append(tokens, Token{Position: position, Class: TokenWord})
			position += end
		}
	}
	return tokens, nil
}

func skipSpaces(source string, position int) (int, error) {
	for position < len(source) && source[position] == ' ' {
		position++
	}
	return position, nil
}

func TestTokeniserSpansCheckHoldsTheDialectToTheSharedScanner(t *testing.T) {
	t.Parallel()

	twoTables := "CREATE TABLE alpha (id integer); CREATE TABLE beta (id integer);"
	testCases := []struct {
		name          string
		fixture       Fixture
		notApplicable bool
		problems      []string
	}{
		{
			name:     "a dialect splitting as sqllex does passes",
			fixture:  Fixture{NewDialect: func() dialect.DialectPort { return postgres.New() }, SplitterMigration: twoTables},
			problems: nil,
		},
		{
			name: "a dialect splitting elsewhere than sqllex fails",
			fixture: Fixture{NewDialect: func() dialect.DialectPort { return oneStatementDialect{DialectPort: postgres.New()} },
				SplitterMigration: twoTables},
			problems: []string{"SplitterMigration: the statements differ: sqllex finds (offset, length) [{0 31} {33 30}], " +
				"ParseStatements [{0 64}]"},
		},
		{
			name: "a tokeniser reading a string as something else fails",
			fixture: Fixture{NewDialect: func() dialect.DialectPort { return postgres.New() }, ParamQuery: "SELECT 'a b' x",
				Tokeniser: &TokeniserFixture{Tokens: wordsAndStrings, SkipNoise: skipSpaces}},
			problems: []string{`ParamQuery: byte 7 ("'a b' x"): sqllex reads a string here but the dialect does not`},
		},
		{
			name:          "a fixture without SQL is not checked",
			fixture:       Fixture{NewDialect: func() dialect.DialectPort { return postgres.New() }},
			notApplicable: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			outcome := checkTokeniserSpans(t.Context(), checkEnvironment{}, &testCase.fixture)

			require.Equal(t, testCase.notApplicable, outcome.NotApplicable != "")
			require.Equal(t, testCase.problems, outcome.Problems)
		})
	}
}
