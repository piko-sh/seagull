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

package dynamic

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/mysql"
)

func filterInfo() dialect.Facts {
	info := testDialectInfo(dollar, dialect.PlaceholderSigil{})
	info.Filters = dialect.FilterSyntax{
		Operators:         []string{"ILIKE", "?", "?|", "?&"},
		JSONPathFunctions: []string{"json_extract"},
		Casts:             []dialect.CastForm{{Before: "CAST(", Middle: " AS ", After: ")"}, {Before: "(", Middle: ")::", After: ""}},
		CastTypes:         []string{"boolean", "bool", "numeric", "integer", "int", "bigint", "smallint", "real", "double precision", "text", "blob"},
	}
	info.JSONOperators = []string{"->", "->>"}
	return info
}

func compiledColumnExpression(t *testing.T) *regexp.Regexp {
	t.Helper()
	return regexp.MustCompile(RuntimeSyntax(testEmission(filterInfo(), textMappings(), testDriver())).ColumnExpression)
}

func TestRuntimeColumnExpressionPatternAcceptsSafeForms(t *testing.T) {
	t.Parallel()

	compiled := compiledColumnExpression(t)
	for _, sample := range []string{
		"id",
		"created_at",
		"_underscored",
		"data->'foo'",
		"data->>'foo'",
		"data->'meta'->>'kind'",
		"data->0",
		"data->'items'->0->>'name'",
		"json_extract(data, '$.foo')",
		"json_extract(data,'$.foo.bar')",
		"json_extract(payload,   '$.nested[0].key')",

		"(data)::boolean",
		"(data->>'foo')::boolean",
		"(data->>'foo')::numeric",
		"(data->'meta'->>'kind')::text",
		"(data->>'count')::integer",

		"CAST(data AS REAL)",
		"CAST(data AS INTEGER)",
		"CAST(json_extract(data, '$.foo') AS REAL)",
		"CAST(json_extract(data, '$.count') AS INTEGER)",

		"cast(json_extract(data, '$.foo') as real)",
		"(data->>'flag')::BOOL",
		"CAST(data AS double precision)",
		"cast( data  as  DOUBLE   PRECISION )",
	} {
		assert.Truef(t, compiled.MatchString(sample), "expected %q to be accepted", sample)
	}
}

func TestRuntimeColumnExpressionPatternAcceptsUnicodeIdentifiers(t *testing.T) {
	t.Parallel()

	compiled := compiledColumnExpression(t)

	for _, sample := range []string{
		"naïve",
		"café",
		"имя",
		"名前",
		"über_count",
		"naïve->>'foo'",
		"(café)::text",
		"CAST(café AS TEXT)",
		"json_extract(café, '$.foo')",
		"ключ2",
	} {
		assert.Truef(t, compiled.MatchString(sample), "expected unicode column %q to be accepted", sample)
	}
}

func TestRuntimeColumnExpressionPatternRejectsInjection(t *testing.T) {
	t.Parallel()

	compiled := compiledColumnExpression(t)

	for _, sample := range []string{
		"category OR 1=1",
		"category->>'x' OR 1=1",
		"category;DROP TABLE users",
		"category UNION SELECT password FROM users",
		"category->>'x''; DROP TABLE x; --",
		"json_extract(data, '$.x') OR 1=1",
		"json_extract(data, '$.x'); --",
		"json_extract(data, ''); DROP TABLE x; --')",
		"json_extract(data, '$.x'/*injected*/)",
		"data->'x'/*comment*/",
		"data --comment",
		"data /* hidden */",
		"",
		"   ",
		"\t",
		"data->>",
		"->'foo'",
		"json_extract(, '$.foo')",
		"json_extract(data)",
		"1=1",
		"select 1",
		"data->'unclosed",
		"data->>unquoted",

		"(data)::pg_class",
		"(data)::int; DROP TABLE users",
		"(data)::; --",
		"CAST(data AS bogus_type)",
		"CAST(data AS REAL); DELETE FROM users",
		"CAST(data AS REAL) OR 1=1",
		"((data)::int)::text",
		"(1 + 1)::int",
		"(SELECT 1)::int",
		"CAST((SELECT 1) AS REAL)",
		"CAST(data AS doubleprecision)",
	} {
		assert.Falsef(t, compiled.MatchString(strings.TrimSpace(sample)), "expected %q to be rejected", sample)
	}
}

func TestRuntimeSyntaxTakesEveryFactFromTheDialect(t *testing.T) {
	t.Parallel()

	info := filterInfo()
	info.Lists = dialect.ListSyntax{EmptyMembership: "IN (NONE)", EmptyExclusion: "NOT IN (NONE)"}
	info.Pagination = dialect.PaginationSyntax{Limit: dialect.Affix{Before: "LIMIT ", After: ""}, Offset: dialect.Affix{Before: "OFFSET ", After: ""}, UnboundedLimit: "-1", OffsetFirst: false}
	info.Placeholders.Execution = dialect.PlaceholderSigil{Prefix: "?", Kind: dialect.PlaceholderAnonymous}

	syntax := RuntimeSyntax(testEmission(info, textMappings(), testDriver()))

	require.Equal(t, info.Lists, syntax.Lists)
	require.Equal(t, info.Pagination, syntax.Pagination)
	require.Equal(t, info.Placeholders.Execution, syntax.Form)
	require.Equal(t, info.Filters.Casts, syntax.Casts)
	require.Equal(t, info.Filters.CastTypes, syntax.CastTypes)
	require.Contains(t, syntax.Operators, "ILIKE")
	require.NotContains(t, syntax.Operators, "?", "an operator holding the anonymous placeholder's text is left out")
	require.Equal(t, []string{"json_extract("}, syntax.JSONPathOpenings)
	require.Equal(t, []string{"->"}, syntax.RootOperators)
	require.Len(t, syntax.CastPatterns, 2)
	cast := regexp.MustCompile(syntax.CastPatterns[1])
	require.Equal(t, []string{"(data->>'n')::INT", "data->>'n'", "INT"}, cast.FindStringSubmatch("(data->>'n')::INT"))
}

func TestRuntimeColumnExpressionRefusesABackslashInAQuotedLiteralOnMySQLAndMariaDB(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		info dialect.Facts
	}{
		{name: "mysql", info: dialect.Wrap(mysql.New()).Info()},
		{name: "mariadb", info: dialect.Wrap(mariadb.New()).Info()},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			syntax := RuntimeSyntax(testEmission(testCase.info, textMappings(), testDriver()))
			compiled := regexp.MustCompile(syntax.ColumnExpression)
			var accepted, refused []string
			for _, operator := range testCase.info.JSONOperators {
				accepted = append(accepted, "colour"+operator+"'$.a'", "colour"+operator+"0")
				refused = append(refused,
					"colour"+operator+`'\'`,
					"colour"+operator+`'$.a\'`,
					"colour"+operator+`'\\'`,
					"colour"+operator+`'\' OR 1=1 -- '`,
				)
			}
			for _, function := range testCase.info.Filters.JSONPathFunctions {
				accepted = append(accepted, function+"(colour, '$.a')")
				refused = append(refused, function+`(colour, '\')`, function+`(colour, '$.a\')`, function+`(colour,'\\')`)
			}
			require.NotEmpty(t, refused, "the dialect writes a quoted JSON path literal")

			for _, sample := range accepted {
				assert.Truef(t, compiled.MatchString(sample), "expected %q to be accepted", sample)
			}
			for _, sample := range refused {
				assert.Falsef(t, compiled.MatchString(sample), "expected %q to be refused", sample)
			}
		})
	}
}
