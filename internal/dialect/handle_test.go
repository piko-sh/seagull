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

package dialect_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestWrapAndUnwrapHoldTheSameDialect(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{}

	handle := dialect.Wrap(engine)

	assert.Same(t, engine, dialect.Unwrap(handle))
}

func TestTheZeroDialectHoldsNoDialect(t *testing.T) {
	t.Parallel()

	var handle dialect.Dialect

	assert.Nil(t, dialect.Unwrap(handle))
	assert.Equal(t, dialect.Facts{}, handle.Info())
	assert.Equal(t, sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "int8"), handle.NormaliseTypeName("int8"))
	assert.Equal(t, handle, dialect.Wrap(nil))
}

func TestADialectNamesTypesAsTheDialectItHoldsDoes(t *testing.T) {
	t.Parallel()

	var received []int
	engine := &fakedialect.Engine{NormaliseTypeNameFunc: func(name string, modifiers ...int) sqlmodel.SQLType {
		received = modifiers
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, name)
	}}

	normalised := dialect.Wrap(engine).NormaliseTypeName("numeric", 10, 2)

	assert.Equal(t, sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"), normalised)
	assert.Equal(t, []int{10, 2}, received)
}

func TestADialectsInfoHoldsTheFactsOfTheDialectsInfo(t *testing.T) {
	t.Parallel()

	info := dialect.Info{
		Name:            "probe",
		Family:          "probe",
		Comments:        ast.CommentStyle{LinePrefix: "#"},
		IdentifierQuote: dialect.IdentifierQuote{Open: "`", Close: "`"},
		OperatorFeatures: map[string]ast.ExpressionFeature{
			"->>": ast.SQLFeatureJSONOp, "->": ast.SQLFeatureJSONOp, "||": ast.SQLFeatureBinaryArithmetic,
		},
		Drivers:          []string{"example.com/driver"},
		SchemaSearchPath: []string{"main"},
		TextType:         "TEXT",
		ToJSON:           "to_json",
		MaxParameters:    999,
		Returning:        ast.DataChangeInsert,
		Identifiers:      sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: true},
		Lexical:          sqllex.Rules{Terminator: ";"},
		AsyncMutations:   true,
	}

	facts := dialect.Wrap(&fakedialect.Engine{InfoFunc: func() dialect.Info { return info }}).Info()

	assert.Equal(t, "probe", facts.Name)
	assert.Equal(t, "probe", facts.Family)
	assert.Equal(t, "#", facts.CommentPrefix)
	assert.Equal(t, info.IdentifierQuote, facts.IdentifierQuote)
	assert.Equal(t, []string{"->", "->>"}, facts.JSONOperators)
	assert.Equal(t, info.Drivers, facts.Drivers)
	assert.Equal(t, "TEXT", facts.TextType)
	assert.Equal(t, "to_json", facts.ToJSON)
	assert.Equal(t, 999, facts.MaxParameters)
	assert.Equal(t, ast.DataChangeInsert, facts.Returning)
	assert.Equal(t, info.Identifiers, facts.Identifiers)
	assert.Equal(t, ";", facts.Lexical.Terminator)
}

func TestInfoFactsShareNoSliceWithTheInfo(t *testing.T) {
	t.Parallel()

	info := dialect.Info{
		Placeholders: dialect.Placeholders{Sigils: []dialect.PlaceholderSigil{{Prefix: "$"}}},
		Filters: dialect.FilterSyntax{
			Operators:         []string{"ILIKE"},
			JSONPathFunctions: []string{"json_extract"},
			Casts:             []dialect.CastForm{{Before: "CAST(", Middle: " AS ", After: ")"}},
			CastTypes:         []string{"integer"},
		},
		Drivers: []string{"example.com/driver"},
		Lexical: sqllex.Rules{Parameters: sqllex.ParameterRules{BracedLiteralWords: []string{"Array"}}},
	}

	facts := info.Facts()
	facts.Placeholders.Sigils[0].Prefix = "changed"
	facts.Filters.Operators[0] = "changed"
	facts.Filters.JSONPathFunctions[0] = "changed"
	facts.Filters.Casts[0].Before = "changed"
	facts.Filters.CastTypes[0] = "changed"
	facts.Drivers[0] = "changed"
	facts.Lexical.Parameters.BracedLiteralWords[0] = "changed"

	require.Equal(t, "$", info.Placeholders.Sigils[0].Prefix)
	require.Equal(t, "ILIKE", info.Filters.Operators[0])
	require.Equal(t, "json_extract", info.Filters.JSONPathFunctions[0])
	require.Equal(t, "CAST(", info.Filters.Casts[0].Before)
	require.Equal(t, "integer", info.Filters.CastTypes[0])
	require.Equal(t, "example.com/driver", info.Drivers[0])
	require.Equal(t, "Array", info.Lexical.Parameters.BracedLiteralWords[0])
}

func TestTheFactsOfANilInfoAreTheZeroFacts(t *testing.T) {
	t.Parallel()

	var info *dialect.Info

	require.Equal(t, dialect.Facts{}, info.Facts())
}
