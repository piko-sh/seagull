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

package postgres

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestApplyDDL_CreateFunctionParameterModes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantArguments  []string
		wantReturnType string
		wantColumns    []string
		wantVariadic   bool
		wantReturnsSet bool
	}{
		{
			name:           "OUT parameters are not callable arguments",
			sql:            "CREATE FUNCTION f(IN a int, OUT b text, OUT c int) LANGUAGE sql AS 'SELECT 1'",
			wantArguments:  []string{"a"},
			wantReturnType: "",
			wantColumns:    []string{"b", "c"},
		},
		{
			name:           "a single OUT parameter is the return type",
			sql:            "CREATE FUNCTION f(a int, OUT total int8) LANGUAGE sql AS 'SELECT 1'",
			wantArguments:  []string{"a"},
			wantReturnType: "int8",
		},
		{
			name:           "INOUT is both an argument and a result",
			sql:            "CREATE FUNCTION f(INOUT a int, b text) RETURNS record LANGUAGE sql AS 'SELECT 1'",
			wantArguments:  []string{"a", "b"},
			wantReturnType: "int4",
		},
		{
			name:           "set-returning OUT parameters become a row type",
			sql:            "CREATE FUNCTION f(a int, OUT b text, OUT c int) RETURNS SETOF record LANGUAGE sql AS 'SELECT 1'",
			wantArguments:  []string{"a"},
			wantReturnType: "",
			wantColumns:    []string{"b", "c"},
			wantReturnsSet: true,
		},
		{
			name:           "an explicit return type keeps precedence",
			sql:            "CREATE FUNCTION f(a int, OUT b text) RETURNS text LANGUAGE sql AS 'SELECT 1'",
			wantArguments:  []string{"a"},
			wantReturnType: "text",
		},
		{
			name:           "RETURNS TABLE keeps its own columns",
			sql:            "CREATE FUNCTION f(a int, OUT b text, OUT c int) RETURNS TABLE (d int) LANGUAGE sql AS 'SELECT 1'",
			wantArguments:  []string{"a"},
			wantReturnType: "",
			wantColumns:    []string{"d"},
			wantReturnsSet: true,
		},
		{
			name:           "VARIADIC is a callable argument",
			sql:            "CREATE FUNCTION f(a int, VARIADIC rest int[]) RETURNS int LANGUAGE sql AS 'SELECT 1'",
			wantArguments:  []string{"a", "rest"},
			wantReturnType: "int4",
			wantVariadic:   true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation.Function)
			signature := mutation.Function
			assert.Equal(t, testCase.wantArguments, argumentNames(signature.Arguments))
			assert.Equal(t, testCase.wantReturnType, signature.ReturnType.EngineName)
			assert.Equal(t, testCase.wantColumns, columnNames(mutation.Columns))
			assert.Equal(t, testCase.wantVariadic, signature.IsVariadic)
			assert.Equal(t, testCase.wantReturnsSet, signature.ReturnsSet)
		})
	}
}

func TestOutputParametersNameTheResult(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantResultName string
		wantReturnType string
		wantColumns    []string
	}{
		{name: "a single OUT parameter names the result", sql: "CREATE FUNCTION f(a int, OUT total int8) LANGUAGE sql AS 'SELECT 1'",
			wantResultName: "total", wantReturnType: "int8"},
		{name: "a single INOUT parameter names the result", sql: "CREATE FUNCTION f(INOUT a int) LANGUAGE sql AS 'SELECT 1'",
			wantResultName: "a", wantReturnType: "int4"},
		{name: "a declared return type names nothing", sql: "CREATE FUNCTION f(a int) RETURNS int LANGUAGE sql AS 'SELECT 1'",
			wantResultName: "", wantReturnType: "int4"},
		{name: "several OUT parameters of RETURNS record become the row", sql: "CREATE FUNCTION f(OUT a int, OUT b text) RETURNS record LANGUAGE sql AS 'SELECT 1, 2'",
			wantResultName: "", wantReturnType: "", wantColumns: []string{"a", "b"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation.Function)
			require.Equal(t, testCase.wantResultName, mutation.Function.ResultName)
			require.Equal(t, testCase.wantReturnType, mutation.Function.ReturnType.EngineName)
			require.Equal(t, testCase.wantColumns, columnNames(mutation.Columns))
		})
	}
}

func TestArrayTypeOf(t *testing.T) {
	t.Parallel()

	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")

	testCases := []struct {
		name       string
		wantEngine string
		dimensions int
		wantDepth  int
	}{
		{name: "zero dimensions keeps the element", dimensions: 0, wantEngine: "text", wantDepth: 0},
		{name: "one dimension", dimensions: 1, wantEngine: "text[]", wantDepth: 1},
		{name: "two dimensions", dimensions: 2, wantEngine: "text[][]", wantDepth: 2},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wrapped := arrayTypeOf(text, testCase.dimensions)

			assert.Equal(t, testCase.wantEngine, wrapped.EngineName)
			depth := 0
			for current := wrapped; current.Category == sqlmodel.TypeCategoryArray; current = *current.ElementType {
				depth++
			}
			assert.Equal(t, testCase.wantDepth, depth)
		})
	}
}

func argumentNames(arguments []sqlmodel.FunctionArgument) []string {
	if len(arguments) == 0 {
		return nil
	}
	names := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		names = append(names, argument.Name)
	}
	return names
}

func columnNames(columns []sqlmodel.Column) []string {
	if len(columns) == 0 {
		return nil
	}
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
	}
	return names
}

func TestApplyDDL_CreateAndDropDomain(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantSchema     string
		wantName       string
		wantBase       string
		wantDimensions int
		wantNotNull    bool
	}{
		{name: "a plain domain", sql: "CREATE DOMAIN email AS text", wantName: "email", wantBase: "text"},
		{name: "AS is optional", sql: "CREATE DOMAIN email text NOT NULL", wantName: "email", wantBase: "text", wantNotNull: true},
		{name: "constraints and defaults are skipped", wantName: "code", wantBase: "varchar", wantNotNull: true,
			sql: "CREATE DOMAIN code AS varchar(10) DEFAULT 'x' CONSTRAINT short CHECK (char_length(VALUE) < 10 AND VALUE IS NOT NULL) NOT NULL COLLATE \"C\""},
		{name: "a NOT NULL inside CHECK is not the domain's", wantName: "code", wantBase: "text",
			sql: "CREATE DOMAIN code AS text CHECK (VALUE IS NOT NULL)"},
		{name: "a qualified array domain", sql: "CREATE DOMAIN billing.tags AS text[]", wantSchema: "billing", wantName: "tags", wantBase: "text", wantDimensions: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			assert.Equal(t, ast.MutationCreateDomain, mutation.Kind)
			assert.Equal(t, testCase.wantSchema, mutation.SchemaName)
			assert.Equal(t, testCase.wantName, mutation.EnumName)
			require.Len(t, mutation.Columns, 1)
			assert.Equal(t, testCase.wantBase, mutation.Columns[0].SQLType.EngineName)
			assert.Equal(t, testCase.wantDimensions, mutation.Columns[0].ArrayDimensions)
			assert.Equal(t, !testCase.wantNotNull, mutation.Columns[0].Nullable)
		})
	}

	dropped := applyDDL(t, "DROP DOMAIN IF EXISTS billing.tags CASCADE")
	require.NotNil(t, dropped)
	assert.Equal(t, ast.MutationDropType, dropped.Kind)
	assert.Equal(t, "billing", dropped.SchemaName)
	assert.Equal(t, "tags", dropped.EnumName)
}

func TestAFunctionArgumentDefaultIsWrittenWithDefaultOrAnEqualsSign(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		names    []string
		optional []bool
	}{
		{name: "DEFAULT after a named argument", sql: "CREATE FUNCTION f(a int, b text DEFAULT 'x') RETURNS int LANGUAGE sql AS 'SELECT 1'",
			names: []string{"a", "b"}, optional: []bool{false, true}},
		{name: "an equals sign after a named argument", sql: "CREATE FUNCTION f(a int, b text = '!') RETURNS int LANGUAGE sql AS 'SELECT 1'",
			names: []string{"a", "b"}, optional: []bool{false, true}},
		{name: "an equals sign after an unnamed argument", sql: "CREATE FUNCTION f(int, text = lower('X')) RETURNS int LANGUAGE sql AS 'SELECT 1'",
			names: []string{"", ""}, optional: []bool{false, true}},
		{name: "an equals sign after an array argument", sql: "CREATE FUNCTION f(a int[] = '{}', b int = 2) RETURNS int LANGUAGE sql AS 'SELECT 1'",
			names: []string{"a", "b"}, optional: []bool{true, true}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation.Function)
			optional := make([]bool, 0, len(mutation.Function.Arguments))
			for _, argument := range mutation.Function.Arguments {
				optional = append(optional, argument.IsOptional)
			}
			require.Equal(t, testCase.names, argumentNames(mutation.Function.Arguments))
			require.Equal(t, testCase.optional, optional)
		})
	}
}

func TestFieldListsPastTheCompositeLimitFailTheStatement(t *testing.T) {
	t.Parallel()

	fields := func(count int) string {
		parts := make([]string, count)
		for index := range parts {
			parts[index] = "f" + strconv.Itoa(index) + " text"
		}
		return strings.Join(parts, ", ")
	}
	testCases := []struct {
		name        string
		sql         string
		wantToken   string
		wantMessage string
	}{
		{
			name:        "a composite type with one field too many",
			sql:         "CREATE TYPE wide AS (" + fields(maxReturnsTableColumns+1) + ")",
			wantToken:   "f1664",
			wantMessage: "a composite type holds at most 1664 fields",
		},
		{
			name:        "a function returning a table with one column too many",
			sql:         "CREATE FUNCTION wide() RETURNS TABLE (" + fields(maxReturnsTableColumns+1) + ") LANGUAGE sql AS $$ SELECT 1 $$",
			wantToken:   "f1664",
			wantMessage: "a function returns a table of at most 1664 columns",
		},
		{
			name:        "a function returning a table whose column has no name",
			sql:         "CREATE FUNCTION f() RETURNS TABLE (a int, , b text) LANGUAGE sql AS $$ SELECT 1, 'b' $$",
			wantToken:   ",",
			wantMessage: "expected identifier",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDLWith(t, New(), testCase.sql)

			assert.Nil(t, mutation)
			parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, isParseError, "got %v", err)
			assert.Equal(t, testCase.wantToken, parseError.Token)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestFieldListsAtTheCompositeLimitAreKeptWhole(t *testing.T) {
	t.Parallel()

	parts := make([]string, maxReturnsTableColumns)
	for index := range parts {
		parts[index] = "f" + strconv.Itoa(index) + " text"
	}
	fields := strings.Join(parts, ", ")
	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a composite type", sql: "CREATE TYPE wide AS (" + fields + ")"},
		{name: "a function returning a table", sql: "CREATE FUNCTION wide() RETURNS TABLE (" + fields + ") LANGUAGE sql AS $$ SELECT 1 $$"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDLWith(t, New(), testCase.sql)

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Len(t, mutation.Columns, maxReturnsTableColumns)
		})
	}
}

func TestDropTypeFunctionAndSchemaRecordWhetherTheTargetMayBeMissing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantIfExists bool
	}{
		{name: "DROP TYPE IF EXISTS", sql: "DROP TYPE IF EXISTS mood CASCADE", wantIfExists: true},
		{name: "DROP TYPE", sql: "DROP TYPE mood", wantIfExists: false},
		{name: "DROP DOMAIN IF EXISTS", sql: "DROP DOMAIN IF EXISTS email", wantIfExists: true},
		{name: "DROP FUNCTION IF EXISTS", sql: "DROP FUNCTION IF EXISTS f(int)", wantIfExists: true},
		{name: "DROP PROCEDURE", sql: "DROP PROCEDURE p", wantIfExists: false},
		{name: "DROP SCHEMA IF EXISTS", sql: "DROP SCHEMA IF EXISTS app CASCADE", wantIfExists: true},
		{name: "DROP SCHEMA", sql: "DROP SCHEMA app", wantIfExists: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantIfExists, mutation.IfExists)
		})
	}
}
