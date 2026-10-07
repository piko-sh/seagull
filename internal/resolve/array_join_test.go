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

package resolve

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type arrayJoinEntriesEngine struct {
	*fakedialect.Engine
}

func (arrayJoinEntriesEngine) ArrayJoinElement(source sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if source.Category != sqlmodel.TypeCategoryMap || source.KeyType == nil || source.ElementType == nil {
		return sqlmodel.SQLType{}, false
	}
	return arrayJoinTuple(
		sqlmodel.StructField{Name: "keys", SQLType: *source.KeyType},
		sqlmodel.StructField{Name: "values", SQLType: *source.ElementType},
	), true
}

func arrayJoinArrayOf(element sqlmodel.SQLType) sqlmodel.SQLType {
	array := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "Array("+element.EngineName+")")
	array.ElementType = &element
	return array
}

func arrayJoinNullable(sqlType sqlmodel.SQLType) sqlmodel.SQLType {
	sqlType.Nullable = true
	return sqlType
}

func arrayJoinTuple(fields ...sqlmodel.StructField) sqlmodel.SQLType {
	tuple := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "Tuple")
	tuple.StructFields = fields
	return tuple
}

func arrayJoinText() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "String")
}

func arrayJoinUnsigned() sqlmodel.SQLType {
	return sqlmodel.NewIntegerType("UInt32", 32, true)
}

func arrayJoinSigned() sqlmodel.SQLType {
	return sqlmodel.NewIntegerType("Int32", 32, false)
}

func arrayJoinNestedTuple() sqlmodel.SQLType {
	return arrayJoinTuple(
		sqlmodel.StructField{Name: "x", SQLType: arrayJoinUnsigned()},
		sqlmodel.StructField{Name: "y", SQLType: arrayJoinNullable(arrayJoinText())},
	)
}

func arrayJoinTable() *sqlmodel.Table {
	return &sqlmodel.Table{
		Name: "sightings",
		Columns: []sqlmodel.Column{
			{Name: "id", SQLType: arrayJoinUnsigned()},
			{Name: "tags", SQLType: arrayJoinArrayOf(arrayJoinText())},
			{Name: "counts", SQLType: arrayJoinArrayOf(arrayJoinNullable(arrayJoinSigned()))},
			{Name: "legs", SQLType: arrayJoinArrayOf(arrayJoinNestedTuple()), Hidden: true},
			{Name: "legs.x", SQLType: arrayJoinArrayOf(arrayJoinUnsigned())},
			{Name: "legs.y", SQLType: arrayJoinArrayOf(arrayJoinNullable(arrayJoinText()))},
		},
	}
}

func arrayJoinScope(t *testing.T) (*ScopeBuilder, *ScopeChain) {
	t.Helper()
	engine := &fakedialect.Engine{
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			if name == "Array(UInt8)" {
				return arrayJoinArrayOf(sqlmodel.NewIntegerType("UInt8", 8, true))
			}
			return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name)
		},
	}
	catalogue := fakedialect.NewCatalogue("default")
	builder := NewCatalogueScopeBuilder(engine, catalogue)
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	scope.AddTable(ast.TableReference{Name: "sightings", Alias: "s"}, ast.JoinInner, arrayJoinTable())
	return builder, scope
}

func describeScoped(column *ast.ScopedColumn) string {
	if column == nil {
		return "<none>"
	}
	described := fmt.Sprintf("%s %s", column.Name, column.SQLType.EngineName)
	if column.Nullable {
		described += " null"
	}
	return described
}

func arrayJoinReference(qualifier, column string) *ast.ColumnRefExpression {
	return &ast.ColumnRefExpression{TableAlias: qualifier, ColumnName: column}
}

func TestResolveArrayJoinClausesTypeEveryNameAsTheEngineReadsIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		clauses []ast.RawArrayJoinClause
		lookups map[string]string
		star    []string
	}{
		{
			name:    "a column unfolded in place names its element where the star lists it",
			clauses: []ast.RawArrayJoinClause{{Source: arrayJoinReference("", "tags"), Alias: "tags", SourceColumn: "tags"}},
			lookups: map[string]string{"tags": "tags String", "s.tags": "tags String", "counts": "counts Array(Int32)"},
			star:    []string{"id UInt32", "tags String", "counts Array(Int32)", "legs.x Array(UInt32)", "legs.y Array(String)"},
		},
		{
			name:    "an alias names the element and leaves the column and the star as they are",
			clauses: []ast.RawArrayJoinClause{{Source: arrayJoinReference("", "tags"), Alias: "tag", SourceColumn: "tags"}},
			lookups: map[string]string{"tag": "tag String", "tags": "tags Array(String)"},
			star:    []string{"id UInt32", "tags Array(String)", "counts Array(Int32)", "legs.x Array(UInt32)", "legs.y Array(String)"},
		},
		{
			name: "a left array join gives an empty array the element's default rather than NULL",
			clauses: []ast.RawArrayJoinClause{
				{Source: arrayJoinReference("", "tags"), Alias: "tag", SourceColumn: "tags", IsLeft: true},
				{Source: arrayJoinReference("", "counts"), Alias: "amount", SourceColumn: "counts", IsLeft: true},
			},
			lookups: map[string]string{"tag": "tag String", "amount": "amount Int32 null"},
			star:    []string{"id UInt32", "tags Array(String)", "counts Array(Int32)", "legs.x Array(UInt32)", "legs.y Array(String)"},
		},
		{
			name: "a nested structure unfolded in place unfolds the columns it is flattened into",
			clauses: []ast.RawArrayJoinClause{{
				Source: arrayJoinReference("", "legs"), Alias: "legs", SourceColumn: "legs", ElementPrefix: "legs.",
			}},
			lookups: map[string]string{"legs": "legs Tuple", "legs.x": "legs.x UInt32", "legs.y": "legs.y String null", "tags": "tags Array(String)"},
			star:    []string{"id UInt32", "tags Array(String)", "counts Array(Int32)", "legs.x UInt32", "legs.y String"},
		},
		{
			name:    "an aliased nested structure names its tuple and leaves its columns as arrays",
			clauses: []ast.RawArrayJoinClause{{Source: arrayJoinReference("", "legs"), Alias: "leg", SourceColumn: "legs"}},
			lookups: map[string]string{"leg": "leg Tuple", "legs.x": "legs.x Array(UInt32)"},
			star:    []string{"id UInt32", "tags Array(String)", "counts Array(Int32)", "legs.x Array(UInt32)", "legs.y Array(String)"},
		},
		{
			name:    "one flattened column of a nested structure unfolds alone",
			clauses: []ast.RawArrayJoinClause{{Source: arrayJoinReference("", "legs.x"), Alias: "legs.x", SourceColumn: "legs.x"}},
			lookups: map[string]string{"legs.x": "legs.x UInt32", "legs.y": "legs.y Array(String)"},
			star:    []string{"id UInt32", "tags Array(String)", "counts Array(Int32)", "legs.x UInt32", "legs.y Array(String)"},
		},
		{
			name:    "a qualified source unfolds the column of the relation it names",
			clauses: []ast.RawArrayJoinClause{{Source: arrayJoinReference("s", "tags"), Alias: "tags", SourceColumn: "tags"}},
			lookups: map[string]string{"tags": "tags String"},
			star:    []string{"id UInt32", "tags String", "counts Array(Int32)", "legs.x Array(UInt32)", "legs.y Array(String)"},
		},
		{
			name: "an expression source is typed by its expression",
			clauses: []ast.RawArrayJoinClause{{
				Source: &ast.CastExpression{Inner: &ast.LiteralExpression{TypeName: ""}, TypeName: "Array(UInt8)"},
				Alias:  "level", SourceExpression: "CAST([1, 2] AS Array(UInt8))",
			}},
			lookups: map[string]string{"level": "level UInt8"},
			star:    []string{"id UInt32", "tags Array(String)", "counts Array(Int32)", "legs.x Array(UInt32)", "legs.y Array(String)"},
		},
		{
			name:    "a source the dialect did not parse is read from its column name",
			clauses: []ast.RawArrayJoinClause{{Alias: "tag", SourceColumn: "tags"}},
			lookups: map[string]string{"tag": "tag String"},
			star:    []string{"id UInt32", "tags Array(String)", "counts Array(Int32)", "legs.x Array(UInt32)", "legs.y Array(String)"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, scope := arrayJoinScope(t)
			require.Empty(t, builder.ResolveArrayJoinClauses(t.Context(), testCase.clauses, scope))
			for reference, want := range testCase.lookups {
				qualifier, column := "", reference
				if reference == "s.tags" {
					qualifier, column = "s", "tags"
				}
				resolved, _, err := scope.ResolveColumn(qualifier, column)
				require.NoError(t, err, reference)
				require.Equal(t, want, describeScoped(resolved), reference)
			}
			columns := scope.StarColumns()
			star := make([]string, len(columns))
			for index := range columns {
				star[index] = columns[index].Column.Name + " " + columns[index].Column.SQLType.EngineName
			}
			require.Equal(t, testCase.star, star)
		})
	}
}

func TestResolveArrayJoinClausesReportsASourceItCannotFind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		clause ast.RawArrayJoinClause
	}{
		{name: "a bare column", clause: ast.RawArrayJoinClause{Source: arrayJoinReference("", "tagz"), Alias: "tag", SourceColumn: "tagz"}},
		{name: "a column the dialect did not parse", clause: ast.RawArrayJoinClause{Alias: "tag", SourceColumn: "tagz"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, scope := arrayJoinScope(t)
			problems := builder.ResolveArrayJoinClauses(t.Context(), []ast.RawArrayJoinClause{testCase.clause}, scope)
			require.Len(t, problems, 1)
			require.Equal(t, diag.CodeUnknownColumn, problems[0].Code)
			require.Equal(t, "tags", problems[0].Suggestion)
			resolved, _, err := scope.ResolveColumn("", "tag")
			require.NoError(t, err)
			require.Equal(t, "tag  null", describeScoped(resolved))
		})
	}
}

func TestResolveArrayJoinClausesKeepsTheCatalogueTableUnchanged(t *testing.T) {
	t.Parallel()

	builder, scope := arrayJoinScope(t)
	table := arrayJoinTable()
	other := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	other.AddTable(ast.TableReference{Name: "sightings"}, ast.JoinInner, table)
	require.Empty(t, builder.ResolveArrayJoinClauses(t.Context(), []ast.RawArrayJoinClause{{
		Source: arrayJoinReference("", "legs"), Alias: "legs", SourceColumn: "legs", ElementPrefix: "legs.",
	}}, other))
	require.Equal(t, "Array(String)", table.Columns[1].SQLType.EngineName)
	require.Equal(t, "Array(UInt32)", table.Columns[4].SQLType.EngineName)
	resolved, _, err := scope.ResolveColumn("", "legs.x")
	require.NoError(t, err)
	require.Equal(t, "legs.x Array(UInt32)", describeScoped(resolved))
}

func TestResolveArrayJoinClausesTypeTheElementTheDialectUnfoldsAValueInto(t *testing.T) {
	t.Parallel()

	key, value := arrayJoinText(), arrayJoinUnsigned()
	attributes := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, "Map(String, UInt32)")
	attributes.KeyType, attributes.ElementType = &key, &value
	table := &sqlmodel.Table{Name: "readings", Columns: []sqlmodel.Column{
		{Name: "id", SQLType: arrayJoinUnsigned()},
		{Name: "attributes", SQLType: attributes},
		{Name: "tags", SQLType: arrayJoinArrayOf(arrayJoinText())},
	}}
	testCases := []struct {
		name    string
		clauses []ast.RawArrayJoinClause
		lookups map[string]string
		star    []string
	}{
		{
			name:    "an alias names the dialect's element of a map",
			clauses: []ast.RawArrayJoinClause{{Source: arrayJoinReference("", "attributes"), Alias: "entry", SourceColumn: "attributes"}},
			lookups: map[string]string{"entry": "entry Tuple", "attributes": "attributes Map(String, UInt32)"},
			star:    []string{"id UInt32", "attributes Map(String, UInt32)", "tags Array(String)"},
		},
		{
			name:    "a map unfolded in place names the dialect's element where the star lists it",
			clauses: []ast.RawArrayJoinClause{{Source: arrayJoinReference("", "attributes"), Alias: "attributes", SourceColumn: "attributes"}},
			lookups: map[string]string{"attributes": "attributes Tuple"},
			star:    []string{"id UInt32", "attributes Tuple", "tags Array(String)"},
		},
		{
			name:    "an array the dialect leaves to the default gives its element",
			clauses: []ast.RawArrayJoinClause{{Source: arrayJoinReference("", "tags"), Alias: "tags", SourceColumn: "tags"}},
			lookups: map[string]string{"tags": "tags String"},
			star:    []string{"id UInt32", "attributes Map(String, UInt32)", "tags String"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := arrayJoinEntriesEngine{Engine: &fakedialect.Engine{}}
			builder := NewCatalogueScopeBuilder(engine, fakedialect.NewCatalogue("default"))
			scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
			scope.AddTable(ast.TableReference{Name: "readings", Alias: "r"}, ast.JoinInner, table)
			require.Empty(t, builder.ResolveArrayJoinClauses(t.Context(), testCase.clauses, scope))
			for reference, want := range testCase.lookups {
				resolved, _, err := scope.ResolveColumn("", reference)
				require.NoError(t, err, reference)
				require.Equal(t, want, describeScoped(resolved), reference)
				if want == reference+" Tuple" {
					require.Len(t, resolved.SQLType.StructFields, 2)
					require.Equal(t, "keys", resolved.SQLType.StructFields[0].Name)
					require.Equal(t, "values", resolved.SQLType.StructFields[1].Name)
				}
			}
			columns := scope.StarColumns()
			star := make([]string, len(columns))
			for index := range columns {
				star[index] = columns[index].Column.Name + " " + columns[index].Column.SQLType.EngineName
			}
			require.Equal(t, testCase.star, star)
		})
	}
}

func TestResolveArrayJoinClausesReportsNothingOfAnInterruption(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		failure     error
		name        string
		wantProblem string
	}{
		{name: "an interrupted call in the source", failure: fmt.Errorf("walk: %w", context.DeadlineExceeded), wantProblem: ""},
		{name: "a call in the source the dialect does not know", failure: errors.New("not resolvable"), wantProblem: diag.CodeUnknownFunction},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: func(context.Context, string, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				return nil, testCase.failure
			}}
			builder := NewCatalogueScopeBuilder(engine, fakedialect.NewCatalogue("default"))
			scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
			clause := ast.RawArrayJoinClause{Source: &ast.FunctionCallExpression{FunctionName: "explode"}, Alias: "element"}

			problems := builder.ResolveArrayJoinClauses(t.Context(), []ast.RawArrayJoinClause{clause}, scope)

			var codes []string
			for _, problem := range problems {
				codes = append(codes, problem.Code)
			}
			if testCase.wantProblem == "" {
				require.Empty(t, codes)
				return
			}
			require.Equal(t, []string{testCase.wantProblem}, codes)
		})
	}
}

func TestResolveArrayJoinClausesResolvesNothingOnceTheContextIsDone(t *testing.T) {
	t.Parallel()

	builder, scope := arrayJoinScope(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	problems := builder.ResolveArrayJoinClauses(ctx, []ast.RawArrayJoinClause{{Source: arrayJoinReference("", "tagz"), Alias: "tag", SourceColumn: "tagz"}}, scope)

	require.Empty(t, problems)
	_, _, err := scope.ResolveColumn("", "tag")
	require.Error(t, err, "the entry was not registered")
}
