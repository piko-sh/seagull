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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestParameterNameCollisionsPreserveBindingsAcrossDialects(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			info := engine.Info()
			for _, named := range info.Placeholders.Sigils {
				if named.Kind != dialect.PlaceholderNamed {
					continue
				}
				first, second := named.Render(1, "a", "Int64"), named.Render(2, "d", "Int64")
				t.Run("reserved suffix/"+named.Prefix, func(t *testing.T) {
					source := "-- sgl.query(Bindings, many)\n-- " + first + " as sgl.param(d)\nSELECT d FROM items WHERE d = " + first + " OR d = " + second + " OR d = " + named.Render(3, "d_2", "Int64") + ";"
					checkCollisionBindings(t, engine, source, []int{0, 1, 2})
				})
				t.Run("renamed/"+named.Prefix, func(t *testing.T) {
					source := "-- sgl.query(Bindings, many)\n-- " + first + " as sgl.param(d)\nSELECT d FROM items WHERE d = " + first + " OR d = " + second + " OR d = " + second + ";"
					checkCollisionBindings(t, engine, source, []int{0, 1, 1})
				})
				for _, positional := range info.Placeholders.Sigils {
					if positional.Kind == dialect.PlaceholderNamed {
						continue
					}
					t.Run(fmt.Sprintf("inferred/%d/%s/%s", positional.Kind, positional.Prefix, named.Prefix), func(t *testing.T) {
						source := "-- sgl.query(Bindings, many)\nSELECT d FROM items WHERE d = " + positional.Render(1, "", "") + " OR d = " + second + " OR d = " + second + ";"
						checkCollisionBindings(t, engine, source, []int{0, 1, 1})
					})
				}
			}
		})
	}
}

func checkCollisionBindings(t *testing.T, engine dialect.DialectPort, source string, want []int) {
	t.Helper()
	query := expressionQuery(t, engine, bindingSeedMigration, source)
	names := make(map[string]bool)
	for _, parameter := range query.Parameters {
		require.False(t, names[parameter.Name], "duplicate parameter name %q", parameter.Name)
		names[parameter.Name] = true
	}
	info := engine.Info()
	rendered, err := emitkit.Render(query.Template, &emitkit.Renderer{Query: query, Quote: info.IdentifierQuote, Lists: info.Lists, Form: info.Placeholders.Execution, Placeholders: info.Placeholders, KeepStars: true})
	require.NoError(t, err)
	tokens, err := significantTokens(info.Lexical, rendered.SQL)
	require.NoError(t, err)
	actual, err := renderedBindingTokens(&info, rendered, tokens)
	require.NoError(t, err)
	expected := []string{"SELECT", "d", "FROM", "items", "WHERE", "d", "=", bindingIdentity(want[0], "Int64", info.Placeholders.BindsByName()), "OR", "d", "=", bindingIdentity(want[1], "Int64", info.Placeholders.BindsByName()), "OR", "d", "=", bindingIdentity(want[2], "Int64", info.Placeholders.BindsByName()), ";"}
	require.Equal(t, expected, actual, "source=%s\nparameters=%+v\nrendered=%+v", source, query.Parameters, rendered)
}

func TestMixedPositionalFormsPreserveBindingsAcrossDialects(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			if !engine.Info().Lexical.Parameters.QuestionMark {
				t.Skip("the dialect does not accept anonymous question-mark placeholders")
			}
			cases := []struct {
				forms [3]string
				want  []int
			}{
				{forms: [3]string{"?1", "?", "?1"}, want: []int{0, 1, 0}},
				{forms: [3]string{"?", "?1", "?"}, want: []int{0, 0, 1}},
				{forms: [3]string{"?2", "?", "?1"}, want: []int{1, 2, 0}},
				{forms: [3]string{"?", "?2", "?"}, want: []int{0, 1, 2}},
				{forms: [3]string{"?3", "?3", "?"}, want: []int{0, 0, 1}},
			}
			for _, testCase := range cases {
				t.Run(fmt.Sprint(testCase.forms), func(t *testing.T) {
					source := "-- sgl.query(Bindings, many)\nSELECT d FROM items WHERE d = " + testCase.forms[0] + " OR d = " + testCase.forms[1] + " OR d = " + testCase.forms[2] + ";"
					checkCollisionBindings(t, engine, source, testCase.want)
				})
			}
		})
	}
}

func TestDollarIdentifiersAcrossDialects(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			info := engine.Info()
			for _, column := range []string{"price$usd", "price$", "price$$usd", "price$1"} {
				t.Run(column, func(t *testing.T) {
					tokens, err := sqllex.Tokens(info.Lexical, column)
					require.NoError(t, err)
					require.Len(t, tokens, 1)
					require.Equal(t, sqllex.KindWord, tokens[0].Kind)
					query := expressionQuery(t, engine, "CREATE TABLE prices ("+column+" BIGINT);",
						"-- sgl.query(Price, many)\nSELECT "+column+" FROM prices;")
					require.Empty(t, query.Parameters)
					require.Len(t, query.OutputColumns, 1)
					require.Equal(t, column, query.OutputColumns[0].Name)
				})
			}
		})
	}
}

func TestDollarParameterNamesAcrossDialects(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			info := engine.Info()
			for _, form := range info.Placeholders.Sigils {
				if form.Kind != dialect.PlaceholderNamed {
					continue
				}
				t.Run(form.Prefix, func(t *testing.T) {
					if form.Prefix == "$" && info.Lexical.Quotes.DollarQuoting {
						t.Skip("a second dollar sign opens a dollar-quoted string")
					}
					first, second := form.Render(1, "a$one", "Int64"), form.Render(2, "b$two", "Int64")
					source := "-- sgl.query(Bindings, many)\nSELECT d FROM items WHERE d = " + first + " OR d = " + second + " OR d = " + first + ";"
					checkCollisionBindings(t, engine, source, []int{0, 1, 0})
				})
			}
		})
	}
}

func TestDollarParameterBoundariesAcrossDialects(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			tokens, err := sqllex.Tokens(engine.Info().Lexical, "SELECT 1$value, 'x'$value, )$value, x:y, x$:y, :name, list[a:b]")
			require.NoError(t, err)
			var parameters []string
			for _, token := range tokens {
				if token.Kind == sqllex.KindParameter {
					parameters = append(parameters, token.Raw)
				}
			}
			want := []string{":name"}
			switch name {
			case "duckdb":
				want = []string{"$value", "$value", "$value", ":name"}
			case "sqlite":
				want = []string{"$value", "$value", "$value", ":y", ":y", ":name"}
			case "clickhouse":
				want = nil
			}
			require.Equal(t, want, parameters)
		})
	}
}

func TestBindingOracleRejectsSwapsMovesAndMissingParameters(t *testing.T) {
	info := postgres.New().Info()
	query := &sqlmodel.AnalysedQuery{Name: "oracle", Parameters: []sqlmodel.QueryParameter{{Number: 1, Name: "a"}, {Number: 2, Name: "b"}}}
	source, err := significantTokens(info.Lexical, "SELECT $1 - $2")
	require.NoError(t, err)
	for _, sql := range []string{"SELECT $2 - $1", "SELECT - $1 $2", "SELECT 1 - 2"} {
		output, err := significantTokens(info.Lexical, sql)
		require.NoError(t, err)
		rendered := emitkit.Rendered{SQL: sql, Arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}}}
		require.NotEmpty(t, templateBindingProblems(&info, query, rendered, source, output), sql)
	}
	output, err := significantTokens(info.Lexical, "SELECT $1 - $2")
	require.NoError(t, err)
	rendered := emitkit.Rendered{Arguments: []emitkit.Argument{{Parameter: 1}, {Parameter: 0}}}
	require.NotEmpty(t, templateBindingProblems(&info, query, rendered, source, output))
	rendered.Arguments = []emitkit.Argument{{Parameter: 0}, {Parameter: 1}}
	require.Empty(t, templateBindingProblems(&info, query, rendered, source, output))
}

func TestBindingOracleRejectsNamedAndInferredIdentityCollisions(t *testing.T) {
	info := sqlite.New().Info()
	query := &sqlmodel.AnalysedQuery{Name: "collision", Parameters: []sqlmodel.QueryParameter{
		{Number: 1, Name: "d"},
		{Number: 2, Name: "d_2", Placeholder: "d"},
		{Number: 3, Name: "p3"},
	}}
	source, err := significantTokens(info.Lexical, "SELECT ?, :d, ?")
	require.NoError(t, err)
	correct, err := significantTokens(info.Lexical, "SELECT ?1, ?2, ?3")
	require.NoError(t, err)
	rendered := emitkit.Rendered{Arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}, {Parameter: 2}}}
	require.Empty(t, templateBindingProblems(&info, query, rendered, source, correct))
	incorrect, err := significantTokens(info.Lexical, "SELECT ?1, ?1, ?2")
	require.NoError(t, err)
	rendered.Arguments = []emitkit.Argument{{Parameter: 0}, {Parameter: 2}}
	require.NotEmpty(t, templateBindingProblems(&info, query, rendered, source, incorrect))
}

func TestBindingOraclePreservesTypedAndSparseParameters(t *testing.T) {
	t.Run("typed named placeholders", func(t *testing.T) {
		info := clickhouse.New().Info()
		query := &sqlmodel.AnalysedQuery{Name: "typed", Parameters: []sqlmodel.QueryParameter{{Number: 1, Name: "value", Placeholder: "value"}}}
		source, err := significantTokens(info.Lexical, "SELECT {value:Int64}")
		require.NoError(t, err)
		output, err := significantTokens(info.Lexical, "SELECT {value:UInt64}")
		require.NoError(t, err)
		rendered := emitkit.Rendered{Arguments: []emitkit.Argument{{Parameter: 0, Name: "value"}}}
		require.NotEmpty(t, templateBindingProblems(&info, query, rendered, source, output))
		require.Empty(t, templateBindingProblems(&info, query, rendered, source, source))
	})
	t.Run("sparse repeated placeholders", func(t *testing.T) {
		info := sqlite.New().Info()
		query := &sqlmodel.AnalysedQuery{Name: "sparse", Parameters: []sqlmodel.QueryParameter{{Number: 7, Name: "a"}, {Number: 2, Name: "b"}}}
		source, err := significantTokens(info.Lexical, "SELECT ?7 - ?2 + ?7")
		require.NoError(t, err)
		output, err := significantTokens(info.Lexical, "SELECT ?1 - ?2 + ?1")
		require.NoError(t, err)
		rendered := emitkit.Rendered{Arguments: []emitkit.Argument{{Parameter: 0}, {Parameter: 1}}}
		require.Empty(t, templateBindingProblems(&info, query, rendered, source, output))
		rendered.Arguments[0].Parameter = 1
		require.NotEmpty(t, templateBindingProblems(&info, query, rendered, source, output))
	})
}
