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

package martian

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqlmodel"
)

var errStopped = errors.New("the run was stopped")

type panickingContext struct {
	context.Context
}

func (panickingContext) Err() error {
	panic("the host's context failed")
}

type laggingContext struct {
	context.Context
	checks     atomic.Int32
	liveChecks int32
}

func (c *laggingContext) Err() error {
	if c.checks.Add(1) <= c.liveChecks {
		return nil
	}
	return context.Canceled
}

type foreignStatement struct{}

func (foreignStatement) IsParsedStatement() {}

type cancelledAfter struct {
	context.Context
	checks atomic.Int32
	live   int32
}

func (c *cancelledAfter) Err() error {
	if c.checks.Add(1) <= c.live {
		return nil
	}
	return context.Canceled
}

func capturingContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	return logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(logs, nil))), logs
}

func logRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any
	decoder := json.NewDecoder(logs)
	for decoder.More() {
		record := map[string]any{}
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

func valuesWithoutColumns() ast.ParsedStatement {
	inserted := &insertStatement{
		into:    tableReference{table: qualifiedName{name: "ITEMS"}},
		values:  []expression{&parameterExpression{number: 1}},
		columns: nil,
	}
	return ast.ParsedStatement{Raw: &statement{insertion: inserted}, Location: 0, Length: 0}
}

func TestInfo(t *testing.T) {
	t.Parallel()

	info := New().Info()
	require.Equal(t, Name, info.Name)
	require.Equal(t, Name, info.Family)
	require.Equal(t, []string{searchPath}, info.SchemaSearchPath)
	require.Equal(t, sqlmodel.IdentifierFoldingUpper, info.Identifiers.Unquoted)
	require.False(t, info.Identifiers.Insensitive)
	require.Equal(t, "--", info.Comments.LinePrefix)
	require.False(t, info.Expressions.Has(ast.SQLFeatureStringConcat))
	require.True(t, info.Expressions.Has(ast.SQLFeatureInList))
	require.Zero(t, info.Returning)
}

func TestNormaliseTypeName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		typeName         string
		modifiers        []int
		expectedName     string
		expectedCategory sqlmodel.SQLTypeCategory
	}{
		{name: "an integer in any case", typeName: "whole32", modifiers: nil, expectedName: typeWhole32, expectedCategory: sqlmodel.TypeCategoryInteger},
		{name: "a float", typeName: "REAL64", modifiers: nil, expectedName: typeReal64, expectedCategory: sqlmodel.TypeCategoryFloat},
		{name: "a boolean", typeName: "FLAG", modifiers: nil, expectedName: typeFlag, expectedCategory: sqlmodel.TypeCategoryBoolean},
		{name: "a timestamp", typeName: "MOMENT", modifiers: nil, expectedName: typeMoment, expectedCategory: sqlmodel.TypeCategoryTemporal},
		{name: "a byte string", typeName: "BLOB", modifiers: nil, expectedName: typeBlob, expectedCategory: sqlmodel.TypeCategoryBinary},
		{name: "an exact decimal with precision only", typeName: "EXACT", modifiers: []int{8}, expectedName: typeExact, expectedCategory: sqlmodel.TypeCategoryDecimal},
		{name: "an unknown type", typeName: "int4", modifiers: nil, expectedName: "INT4", expectedCategory: sqlmodel.TypeCategoryUnknown},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sqlType := New().NormaliseTypeName(testCase.typeName, testCase.modifiers...)
			require.Equal(t, testCase.expectedName, sqlType.EngineName)
			require.Equal(t, testCase.expectedCategory, sqlType.Category)
		})
	}
}

func TestPromoteType(t *testing.T) {
	t.Parallel()

	engine := New()
	whole16 := engine.NormaliseTypeName(typeWhole16)
	whole64 := engine.NormaliseTypeName(typeWhole64)
	real64 := engine.NormaliseTypeName(typeReal64)
	textual := engine.NormaliseTypeName(typeTextual)

	require.Equal(t, typeWhole64, engine.PromoteType(whole16, whole64).EngineName)
	require.Equal(t, typeWhole64, engine.PromoteType(whole64, whole16).EngineName)
	require.Equal(t, typeReal64, engine.PromoteType(whole16, real64).EngineName)
	require.Equal(t, typeTextual, engine.PromoteType(textual, whole16).EngineName)
}

func TestCanImplicitCast(t *testing.T) {
	t.Parallel()

	engine := New()
	require.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
	require.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "")))
	require.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "")))
	require.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
}

func TestBuiltins(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions().Functions
	require.Contains(t, functions, "CONCAT")
	require.True(t, functions["CONCAT"][0].IsVariadic)
	require.True(t, functions["COUNT"][0].IsAggregate)
	require.Equal(t, typeTextual, functions["UPPER"][0].ReturnType.EngineName)

	types := engine.BuiltinTypes().Types
	require.Len(t, types, len(builtinTypes))
	delete(types, typeFlag)
	require.Contains(t, engine.BuiltinTypes().Types, typeFlag)
	require.Nil(t, engine.TableValuedFunctionColumns("anything"))
}

func TestInfoDescribesRendering(t *testing.T) {
	t.Parallel()

	info := New().Info()
	require.Equal(t, "@p7", info.Placeholders.Execution.Render(7, "", ""))
	require.Equal(t, "[odd]]name]", info.IdentifierQuote.Quote("odd]name"))
	require.Equal(t, "IN (SELECT NULL WHERE 1 = 0)", info.Lists.EmptyMembership)
	require.Equal(t, "NOT IN (SELECT NULL WHERE 1 = 0)", info.Lists.EmptyExclusion)
	require.Zero(t, info.Lists.ArrayMembership)
	require.True(t, info.Pagination.OffsetFirst)
	require.Equal(t, "OFFSET 10 ROWS FETCH NEXT 5 ROWS ONLY",
		info.Pagination.Offset.Before+"10"+info.Pagination.Offset.After+" "+info.Pagination.Limit.Before+"5"+info.Pagination.Limit.After)
	require.Len(t, info.Filters.Casts, 1)
	require.Equal(t, "CONVERT(x TO WHOLE32)", info.Filters.Casts[0].Before+"x"+info.Filters.Casts[0].Middle+typeWhole32+info.Filters.Casts[0].After)
	require.Contains(t, info.Filters.CastTypes, typeWhole32)
}

func TestInfoSharesNothingWithTheDialect(t *testing.T) {
	t.Parallel()

	engine := New()
	changed := engine.Info()
	changed.Filters.Casts[0].Before = "CAST("
	changed.Filters.CastTypes[0] = "INTEGER"

	fresh := engine.Info()
	require.Equal(t, "CONVERT(", fresh.Filters.Casts[0].Before)
	require.Equal(t, typeFlag, fresh.Filters.CastTypes[0])
}

func TestEntryPointsReturnAPanicAsAnErrorAndLogItOnce(t *testing.T) {
	t.Parallel()

	creation := parseOne(t, "CREATE TABLE a (x WHOLE32)")
	deletion := parseOne(t, "DELETE FROM items")
	testCases := []struct {
		call      func(ctx context.Context, engine *Dialect) (any, error)
		name      string
		operation string
	}{
		{
			name:      "parse statements under a context whose Err panics",
			operation: "martian: parse statements",
			call: func(ctx context.Context, engine *Dialect) (any, error) {
				return engine.ParseStatements(panickingContext{Context: ctx}, "DELETE FROM items")
			},
		},
		{
			name:      "apply DDL under a context whose Err panics",
			operation: "martian: apply DDL",
			call: func(ctx context.Context, engine *Dialect) (any, error) {
				return engine.ApplyDDL(panickingContext{Context: ctx}, creation)
			},
		},
		{
			name:      "analyse query under a context whose Err panics",
			operation: "martian: analyse query",
			call: func(ctx context.Context, engine *Dialect) (any, error) {
				return engine.AnalyseQuery(panickingContext{Context: ctx}, nil, deletion)
			},
		},
		{
			name:      "analyse query of a statement the parser never builds",
			operation: "martian: analyse query",
			call: func(ctx context.Context, engine *Dialect) (any, error) {
				return engine.AnalyseQuery(ctx, nil, valuesWithoutColumns())
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := capturingContext(t)
			result, err := testCase.call(ctx, New())

			require.ErrorIs(t, err, fault.ErrPanic)
			panicError, isPanic := errors.AsType[*dialectkit.PanicError](err)
			require.True(t, isPanic)
			require.Equal(t, testCase.operation, panicError.Operation)
			require.NotContains(t, err.Error(), "goroutine")
			require.Nil(t, result)
			records := logRecords(t, logs)
			require.Len(t, records, 1)
			assert.Equal(t, "WARN", records[0]["level"])
			assert.Equal(t, testCase.operation, records[0]["operation"])
			assert.NotEmpty(t, records[0]["stack"])
		})
	}
}

func TestEntryPointsStopWithTheCauseOfADoneContext(t *testing.T) {
	t.Parallel()

	creation := parseOne(t, "CREATE TABLE a (x WHOLE32)")
	deletion := parseOne(t, "DELETE FROM items")
	testCases := []struct {
		call func(ctx context.Context, engine *Dialect) (any, error)
		name string
	}{
		{
			name: "parse statements",
			call: func(ctx context.Context, engine *Dialect) (any, error) {
				return engine.ParseStatements(ctx, "DELETE FROM items")
			},
		},
		{
			name: "apply DDL",
			call: func(ctx context.Context, engine *Dialect) (any, error) {
				return engine.ApplyDDL(ctx, creation)
			},
		},
		{
			name: "analyse query",
			call: func(ctx context.Context, engine *Dialect) (any, error) {
				return engine.AnalyseQuery(ctx, nil, deletion)
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(errStopped)
			result, err := testCase.call(ctx, New())

			require.ErrorIs(t, err, errStopped)
			require.Nil(t, result)
		})
	}
}

func TestAnalyseQueryStopsWhileItWalksASelectListOnceTheContextIsDone(t *testing.T) {
	t.Parallel()

	selection := parseOne(t, "SELECT id, name, id, name FROM items")
	testCases := []struct {
		name          string
		live          int32
		expectStopped bool
	}{
		{name: "a context done before the walk", live: 0, expectStopped: true},
		{name: "a context done after the first item", live: 2, expectStopped: true},
		{name: "a context live throughout", live: 100, expectStopped: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := &cancelledAfter{Context: t.Context(), checks: atomic.Int32{}, live: testCase.live}
			analysis, err := New().AnalyseQuery(ctx, nil, selection)
			if testCase.expectStopped {
				require.ErrorIs(t, err, context.Canceled)
				require.Nil(t, analysis)
				return
			}
			require.NoError(t, err)
			require.Len(t, analysis.OutputColumns, 4)
		})
	}
}

func TestParseStatementsStopsBeforeTheNextStatementOnceTheContextIsDone(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantError  error
		name       string
		liveChecks int32
	}{
		{name: "a context live for every statement parses them all", liveChecks: 3, wantError: nil},
		{name: "a context done after the first statement stops the parse", liveChecks: 2, wantError: context.Canceled},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := &laggingContext{Context: t.Context(), checks: atomic.Int32{}, liveChecks: testCase.liveChecks}
			statements, err := New().ParseStatements(ctx, "DELETE FROM a; DELETE FROM b")

			if testCase.wantError == nil {
				require.NoError(t, err)
				require.Len(t, statements, 2)
				return
			}
			require.ErrorIs(t, err, testCase.wantError)
			require.Nil(t, statements)
		})
	}
}

func TestStatementsFromAnotherDialectAreRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		raw         ast.ParsedStatementRaw
		name        string
		wantMessage string
	}{
		{name: "another dialect's statement", raw: foreignStatement{}, wantMessage: "martian.foreignStatement"},
		{name: "no statement at all", raw: nil, wantMessage: "<nil>"},
		{name: "a nil Martian statement", raw: (*statement)(nil), wantMessage: "*martian.statement"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parsed := ast.ParsedStatement{Raw: testCase.raw, Location: 0, Length: 0}
			mutation, err := New().ApplyDDL(t.Context(), parsed)
			require.ErrorIs(t, err, dialect.ErrForeignStatement)
			require.ErrorContains(t, err, testCase.wantMessage)
			require.Nil(t, mutation)
			analysis, err := New().AnalyseQuery(t.Context(), nil, parsed)
			require.ErrorIs(t, err, dialect.ErrForeignStatement)
			require.Nil(t, analysis)
		})
	}
}

func TestAnalyseQueryRefusesDDL(t *testing.T) {
	t.Parallel()

	_, err := New().AnalyseQuery(t.Context(), nil, parseOne(t, "CREATE TABLE a (x WHOLE32)"))
	require.ErrorIs(t, err, errSyntax)
	require.NotErrorIs(t, err, dialect.ErrForeignStatement)
}
