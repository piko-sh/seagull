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

package mysql

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func describeDeclaredParameters(references []ast.RawParameterReference) []string {
	described := make([]string, 0, len(references))
	for _, reference := range references {
		column, declared := "-", "-"
		if reference.ColumnReference != nil {
			column = reference.ColumnReference.TableAlias + "." + reference.ColumnReference.ColumnName
		}
		if reference.CastType != nil {
			declared = reference.CastType.EngineName
		}
		described = append(described, fmt.Sprintf("%d %s %s %s", reference.Number, reference.Context, column, declared))
	}
	return described
}

func TestAParameterConvertedAloneDeclaresTheTypeItIsConvertedTo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a value of an INSERT cast to a type",
			sql:      "INSERT INTO prices (id, born) VALUES (?, CAST(? AS DATE))",
			expected: []string{"1 assignment prices.id -", "2 assignment prices.born date"},
		},
		{
			name:     "a value of an INSERT converted to a type",
			sql:      "INSERT INTO prices (born) VALUES (CONVERT(?, DATE))",
			expected: []string{"1 assignment prices.born date"},
		},
		{
			name:     "a value an UPDATE sets cast to a type",
			sql:      "UPDATE prices SET born = CAST(? AS DATE) WHERE id = ?",
			expected: []string{"1 assignment prices.born date", "2 comparison .id -"},
		},
		{
			name:     "a value an UPDATE sets converted to a type",
			sql:      "UPDATE prices SET born = CONVERT(?, DATE)",
			expected: []string{"1 assignment prices.born date"},
		},
		{
			name:     "a parameter converted to a type and compared",
			sql:      "SELECT id FROM prices WHERE born = CONVERT(?, DATE)",
			expected: []string{"1 cast - date"},
		},
		{
			name:     "a parameter converted to a character set",
			sql:      "SELECT id FROM prices WHERE label = CONVERT(? USING utf8mb4)",
			expected: []string{"1 function-argument - -"},
		},
		{
			name:     "a parameter inside an expression a CAST converts",
			sql:      "UPDATE prices SET born = CAST(CONCAT(?, '') AS DATE)",
			expected: []string{"1 assignment prices.born -"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			require.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}

func TestAParameterConvertedAloneDeclaresTheModifiersOfItsType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantEngineName string
		wantPrecision  *int
		wantScale      *int
		wantLength     *int
	}{
		{name: "a value of an INSERT cast to a precision and a scale",
			sql:            "INSERT INTO prices (amount) VALUES (CAST(? AS DECIMAL(10, 2)))",
			wantEngineName: "decimal", wantPrecision: new(10), wantScale: new(2)},
		{name: "a value of an INSERT converted to a length", sql: "INSERT INTO prices (label) VALUES (CONVERT(?, CHAR(20)))",
			wantEngineName: "char", wantLength: new(20)},
		{name: "a value an UPDATE sets cast to a fractional-second precision",
			sql:            "UPDATE prices SET born = CAST(? AS DATETIME(3))",
			wantEngineName: "datetime", wantPrecision: new(3)},
		{name: "a value an UPDATE sets converted to a target that names no column type",
			sql:            "UPDATE prices SET amount = CONVERT(?, UNSIGNED)",
			wantEngineName: "bigint unsigned"},
		{name: "a list of more modifiers than a type takes is left out",
			sql:            "INSERT INTO prices (amount) VALUES (CAST(? AS DECIMAL(10, 2, 3)))",
			wantEngineName: "decimal"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)
			require.Len(t, analysis.ParameterReferences, 1)
			castType := analysis.ParameterReferences[0].CastType
			require.NotNil(t, castType)

			assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
			assert.Equal(t, testCase.wantEngineName, castType.EngineName)
			assert.Equal(t, testCase.wantPrecision, castType.Precision)
			assert.Equal(t, testCase.wantScale, castType.Scale)
			assert.Equal(t, testCase.wantLength, castType.Length)
		})
	}
}

func TestConditionParameterScansGrowLinearlyWithThePlaceholders(t *testing.T) {
	t.Parallel()

	const smaller = 5_000
	engine := New(WithMaxTokensPerStatement(dialectkit.MaxTokensPerStatementCeiling))

	testCases := []struct {
		name       string
		sql        func(count int) string
		references func(count int) int
	}{
		{
			name: "one call in a condition holding every placeholder",
			sql: func(count int) string {
				return "SELECT id FROM t WHERE id = GREATEST(" + strings.TrimSuffix(strings.Repeat("?, ", count), ", ") + ")"
			},
			references: func(count int) int { return count },
		},
		{
			name: "a condition of many IN lists",
			sql: func(count int) string {
				return "SELECT id FROM t WHERE " + strings.TrimSuffix(strings.Repeat("id IN (?) OR ", count), " OR ")
			},
			references: func(count int) int { return count },
		},
		{
			name: "a condition comparing many calls",
			sql: func(count int) string {
				return "SELECT id FROM t WHERE " + strings.TrimSuffix(strings.Repeat("LEFT(name, 2) = ? OR ", count), " OR ")
			},
			references: func(count int) int { return count },
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analyse := func(count int) func() {
				sql := testCase.sql(count)
				return func() {
					analysis, err := analyseWith(t, engine, sql)
					require.NoError(t, err)
					require.Len(t, analysis.ParameterReferences, testCase.references(count))
				}
			}

			scaling.Linear(t, analyse(smaller), analyse(4*smaller))
		})
	}
}

func TestAParameterConvertedToAMultiWordTypeDeclaresTheWholeType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{name: "a value of an INSERT cast to DOUBLE PRECISION",
			sql:      "INSERT INTO prices (amount) VALUES (CAST(? AS DOUBLE PRECISION))",
			expected: []string{"1 assignment prices.amount double"}},
		{name: "a value of an INSERT converted to SIGNED INTEGER",
			sql:      "INSERT INTO prices (amount) VALUES (CONVERT(?, SIGNED INTEGER))",
			expected: []string{"1 assignment prices.amount bigint"}},
		{name: "a value an UPDATE sets cast to UNSIGNED INTEGER",
			sql:      "UPDATE prices SET amount = CAST(? AS UNSIGNED INTEGER) WHERE id = 1",
			expected: []string{"1 assignment prices.amount bigint unsigned"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}

func TestIsKeywordAtMatchesOnlyAWordAmongTheKeywords(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("a FROM `where` 1 where")
	require.NoError(t, err)
	p := newParser(tokens)
	testCases := []struct {
		name     string
		position int
		keywords []string
		want     bool
	}{
		{name: "a word that is no keyword", position: 0, keywords: []string{keywordFROM, keywordWHERE}, want: false},
		{name: "a keyword among those asked", position: 1, keywords: []string{keywordWHERE, keywordFROM}, want: true},
		{name: "a keyword not among those asked", position: 1, keywords: []string{keywordWHERE}, want: false},
		{name: "a keyword written in quotes", position: 2, keywords: []string{keywordWHERE}, want: false},
		{name: "a number", position: 3, keywords: []string{keywordWHERE}, want: false},
		{name: "a keyword written in lower case", position: 4, keywords: []string{keywordWHERE}, want: true},
		{name: "a position past the end", position: len(tokens) + 1, keywords: []string{keywordWHERE}, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, p.isKeywordAt(testCase.position, testCase.keywords...))
		})
	}
}

func TestCollectCastTypeTokensJoinsTheWordsOfATypeUpToWhatEndsIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		start   int
		want    string
		wantEnd int
	}{
		{name: "a one-word type closed by a parenthesis", sql: "DATE) x", start: 0, want: "DATE", wantEnd: 1},
		{name: "a two-word type closed by a parenthesis", sql: "UNSIGNED INTEGER) x", start: 0, want: "UNSIGNED INTEGER",
			wantEnd: 2},
		{name: "a type followed by a clause keyword", sql: "DOUBLE PRECISION FROM t", start: 0, want: "DOUBLE PRECISION",
			wantEnd: 2},
		{name: "a type followed by a lower-case clause keyword", sql: "SIGNED where x", start: 0, want: "SIGNED", wantEnd: 1},
		{name: "a type closed by a comma", sql: "CHAR, x", start: 0, want: "CHAR", wantEnd: 1},
		{name: "a type at the end of the input", sql: "BINARY", start: 0, want: "BINARY", wantEnd: 1},
		{name: "a type starting past the first word", sql: "AS DECIMAL) x", start: 1, want: "DECIMAL", wantEnd: 2},
		{name: "a type followed by its modifiers", sql: "DECIMAL(10, 2)) x", start: 0, want: "DECIMAL", wantEnd: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)

			words, end := newParser(tokens).collectCastTypeTokens(testCase.start)

			require.Equal(t, testCase.want, words)
			require.Equal(t, testCase.wantEnd, end)
		})
	}
}
