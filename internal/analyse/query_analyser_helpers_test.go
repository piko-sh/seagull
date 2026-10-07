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

package analyse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestIsDynamicQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		parameters  []sqlmodel.QueryParameter
		wantDynamic bool
	}{
		{
			name:        "no parameters yields not dynamic",
			parameters:  nil,
			wantDynamic: false,
		},
		{
			name: "regular param does not set dynamic",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "user_id"},
			},
			wantDynamic: false,
		},
		{
			name: "slice param does not set dynamic",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "ids", IsSlice: true},
			},
			wantDynamic: false,
		},
		{
			name: "optional param sets dynamic",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "email", IsOptional: true},
			},
			wantDynamic: true,
		},
		{
			name: "sortable param sets dynamic",
			parameters: []sqlmodel.QueryParameter{
				{
					Number:          1,
					Name:            "sort",
					Kind:            sqlmodel.ParameterDirectiveSortable,
					SortableColumns: []string{"name", "created_at"},
				},
			},
			wantDynamic: true,
		},
		{
			name: "limit param with max bound sets dynamic",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "page_size", Context: sqlmodel.ParameterContextLimit, MaxLimit: new(100)},
			},
			wantDynamic: true,
		},
		{
			name: "limit param without clamp config is not dynamic",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "limit", Context: sqlmodel.ParameterContextLimit},
			},
			wantDynamic: false,
		},
		{
			name: "offset param without clamp config is not dynamic",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "page_offset", Context: sqlmodel.ParameterContextOffset},
			},
			wantDynamic: false,
		},
		{
			name: "combined dynamic params set dynamic",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "user_id"},
				{Number: 2, Name: "email", IsOptional: true},
				{Number: 3, Name: "sort", Kind: sqlmodel.ParameterDirectiveSortable, SortableColumns: []string{"name"}},
				{Number: 4, Name: "page_size", Context: sqlmodel.ParameterContextLimit, DefaultLimit: new(20)},
				{Number: 5, Name: "page_offset", Context: sqlmodel.ParameterContextOffset},
				{Number: 6, Name: "order", Kind: sqlmodel.ParameterDirectiveSortable, SortableColumns: []string{"email", "id"}},
			},
			wantDynamic: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.wantDynamic, isDynamicQuery(testCase.parameters))
		})
	}
}

func TestAssembleQuery(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}

	t.Run("basic assembly populates all fields", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")
		analyser := &QueryAnalyser{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		directives := &directive.QueryDirectives{
			GroupByKeys: []string{"user_id"},
		}

		rawAnalysis := &ast.RawQueryAnalysis{
			InsertTable:   "users",
			InsertTarget:  ast.TableReference{Name: "users", Schema: "app"},
			InsertColumns: []string{"id", "name"},
			ReadOnly:      true,
		}

		outputColumns := []sqlmodel.OutputColumn{
			{Name: "id", SQLType: intType},
			{Name: "name", SQLType: textType},
		}

		parameters := []sqlmodel.QueryParameter{
			{Name: "user_id", Number: 1, SQLType: intType},
		}

		got, _ := analyser.assembleQuery(&assembleQueryInput{
			queryName:     "GetUser",
			queryCommand:  sqlmodel.QueryCommandOne,
			block:         sqlfile.QueryBlock{SQL: "SELECT id, name FROM users WHERE id = $1", StartLine: 5},
			filename:      "queries/users.sql",
			outputColumns: outputColumns,
			parameters:    parameters,
			isDynamic:     false,
			directives:    directives,
			rawAnalysis:   rawAnalysis,
		})

		require.NotNil(t, got)
		assert.Equal(t, "GetUser", got.Name)
		assert.Equal(t, sqlmodel.QueryCommandOne, got.Command)
		assert.Equal(t, "SELECT id, name FROM users WHERE id = $1", got.SQL)
		assert.Equal(t, "queries/users.sql", got.Filename)
		assert.Equal(t, 5, got.Line)
		assert.Equal(t, outputColumns, got.OutputColumns)
		assert.Equal(t, parameters, got.Parameters)
		assert.False(t, got.IsDynamic)
		assert.Equal(t, []string{"user_id"}, got.GroupByKey)
		assert.Equal(t, sqlmodel.TableName{Schema: "app", Name: "users"}, got.InsertTarget)
		assert.Equal(t, []string{"id", "name"}, got.InsertColumns)
		assert.True(t, got.ReadOnly)
	})

	t.Run("read-only override takes precedence over raw analysis", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")
		analyser := &QueryAnalyser{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		directives := &directive.QueryDirectives{
			ReadOnlyOverride: new(false),
		}

		rawAnalysis := &ast.RawQueryAnalysis{
			ReadOnly: true,
		}

		got, _ := analyser.assembleQuery(&assembleQueryInput{
			queryName:   "ForceWritable",
			directives:  directives,
			rawAnalysis: rawAnalysis,
			block:       sqlfile.QueryBlock{SQL: "SELECT 1"},
		})

		assert.False(t, got.ReadOnly)
	})

	t.Run("dynamic runtime populates allowed columns from the projection", func(t *testing.T) {
		t.Parallel()

		analyser := &QueryAnalyser{
			engine:    &fakedialect.Engine{},
			catalogue: fakedialect.NewCatalogue("public"),
		}

		directives := &directive.QueryDirectives{
			DynamicRuntime: true,
		}

		got, _ := analyser.assembleQuery(&assembleQueryInput{
			queryName:  "DynamicUsers",
			directives: directives,
			rawAnalysis: &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{{Name: "users"}},
			},
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SourceColumn: "id", SQLType: intType},
				{Name: "email", SourceColumn: "email", SQLType: textType},
			},
			block: sqlfile.QueryBlock{SQL: "SELECT id, email FROM users"},
		})

		require.Len(t, got.AllowedColumns, 2)
		assert.Equal(t, "id", got.AllowedColumns[0].Name)
		assert.Equal(t, "email", got.AllowedColumns[1].Name)
		assert.True(t, got.DynamicRuntime)
	})
}

func TestExtractAllowedColumns(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}

	t.Run("no output columns returns empty", func(t *testing.T) {
		t.Parallel()

		got := extractAllowedColumns(nil)

		assert.Empty(t, got)
	})

	t.Run("keys on output name and emits the qualified source expression", func(t *testing.T) {
		t.Parallel()

		got := extractAllowedColumns([]sqlmodel.OutputColumn{
			{Name: "id", SourceColumn: "id", SourceQualifier: "users", SQLType: intType},
			{Name: "name", SourceColumn: "name", SourceQualifier: "users", SQLType: textType},
		})

		require.Len(t, got, 2)
		assert.Equal(t, "id", got[0].Name)
		assert.Equal(t, "users.id", got[0].SourceExpression)
		assert.Equal(t, intType, got[0].SQLType)
		assert.Equal(t, "name", got[1].Name)
		assert.Equal(t, "users.name", got[1].SourceExpression)
	})

	t.Run("keys on the alias and resolves it to the real source column", func(t *testing.T) {
		t.Parallel()

		got := extractAllowedColumns([]sqlmodel.OutputColumn{
			{Name: "public_handle", SourceColumn: "handle", SourceQualifier: "p", SQLType: textType},
		})

		require.Len(t, got, 1)
		assert.Equal(t, "public_handle", got[0].Name)
		assert.Equal(t, "p.handle", got[0].SourceExpression)
	})

	t.Run("emits a bare source expression when no qualifier is known", func(t *testing.T) {
		t.Parallel()

		got := extractAllowedColumns([]sqlmodel.OutputColumn{
			{Name: "id", SourceColumn: "id", SQLType: intType},
		})

		require.Len(t, got, 1)
		assert.Equal(t, "id", got[0].Name)
		assert.Equal(t, "id", got[0].SourceExpression)
	})

	t.Run("excludes expression columns that have no source column", func(t *testing.T) {
		t.Parallel()

		got := extractAllowedColumns([]sqlmodel.OutputColumn{
			{Name: "id", SourceColumn: "id", SourceQualifier: "t", SQLType: intType},
			{Name: "total", SourceColumn: "", SQLType: intType},
		})

		require.Len(t, got, 1)
		assert.Equal(t, "id", got[0].Name)
	})

	t.Run("deduplicates a repeated output name", func(t *testing.T) {
		t.Parallel()

		got := extractAllowedColumns([]sqlmodel.OutputColumn{
			{Name: "id", SourceColumn: "id", SourceQualifier: "a", SQLType: intType},
			{Name: "id", SourceColumn: "id", SourceQualifier: "b", SQLType: intType},
		})

		require.Len(t, got, 1)
		assert.Equal(t, "id", got[0].Name)
		assert.Equal(t, "a.id", got[0].SourceExpression, "first projected column wins")
	})

	t.Run("a column on a FROM table but absent from the projection is not allowed", func(t *testing.T) {
		t.Parallel()

		got := extractAllowedColumns([]sqlmodel.OutputColumn{
			{Name: "id", SourceColumn: "id", SQLType: intType},
			{Name: "name", SourceColumn: "name", SQLType: textType},
		})

		names := make([]string, len(got))
		for index, column := range got {
			names[index] = column.Name
		}
		assert.NotContains(t, names, "secret")
	})
}

func TestPropagateCatalogueColumnOverridesHonoursTheSourceSchema(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	textType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}
	catalogue.Schemas["public"].Tables["users"] = &sqlmodel.Table{
		Name: "users", Schema: "public", Columns: []sqlmodel.Column{{Name: "code", SQLType: textType}},
	}
	auditColumn := sqlmodel.Column{Name: "code", SQLType: textType, SQLTypeOverride: "uuid", NullableOverride: new(true)}
	catalogue.Schemas["audit"] = &sqlmodel.Schema{
		Name:   "audit",
		Tables: map[string]*sqlmodel.Table{"users": {Name: "users", Schema: "audit", Columns: []sqlmodel.Column{auditColumn}}},
	}
	engine := &fakedialect.Engine{NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUUID, EngineName: name}
	}}
	analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

	columns := analyser.propagateCatalogueColumnOverrides([]sqlmodel.OutputColumn{
		{Name: "code", SQLType: textType, SourceTable: "users", SourceColumn: "code"},
		{Name: "audit_code", SQLType: textType, SourceSchema: "audit", SourceTable: "users", SourceColumn: "code"},
	}, &directive.DirectiveBlock{})

	require.Equal(t, sqlmodel.TypeCategoryText, columns[0].SQLType.Category, "the public column has no override")
	require.False(t, columns[0].Nullable)
	require.Equal(t, sqlmodel.TypeCategoryUUID, columns[1].SQLType.Category, "the audit column's override applies")
	require.True(t, columns[1].Nullable)
}

func TestPropagateCatalogueColumnOverridesFollowsTheProjectedType(t *testing.T) {
	t.Parallel()

	uuidType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUUID, EngineName: "uuid"}
	textType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}
	override := sqlmodel.GoType{Package: "example.com/ids", Name: "ID"}
	testCases := []struct {
		name      string
		projected sqlmodel.SQLType
		nullable  bool
		keeps     bool
	}{
		{name: "the column as it is keeps the override", projected: uuidType, keeps: true},
		{name: "the column made nullable by an outer join keeps the override", projected: uuidType, nullable: true, keeps: true},
		{name: "a cast to the column's own type keeps the override", projected: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUUID, EngineName: "UUID"}, keeps: true},
		{name: "a cast to another type drops the override", projected: textType, keeps: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = &sqlmodel.Table{
				Name: "users", Schema: "public", Columns: []sqlmodel.Column{{
					Name: "id", SQLType: uuidType, GoTypeOverride: new(override), SQLTypeOverride: "uuid", NullableOverride: new(false),
				}},
			}
			engine := &fakedialect.Engine{NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
				return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUUID, EngineName: name}
			}}
			analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})

			columns := analyser.propagateCatalogueColumnOverrides([]sqlmodel.OutputColumn{
				{Name: "id", SQLType: testCase.projected, Nullable: testCase.nullable, SourceTable: "users", SourceColumn: "id"},
			}, &directive.DirectiveBlock{})

			require.False(t, columns[0].Nullable, "a nullable override holds whatever the projection's type")
			if testCase.keeps {
				require.Equal(t, &override, columns[0].GoTypeOverride)
				require.Equal(t, sqlmodel.TypeCategoryUUID, columns[0].SQLType.Category)
				return
			}
			require.Nil(t, columns[0].GoTypeOverride)
			require.Equal(t, textType, columns[0].SQLType)
		})
	}
}

func helperAnalyser(t *testing.T) *QueryAnalyser {
	t.Helper()

	tags := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "text[]")
	tags.ElementType = new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"))
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
		sqlmodel.Column{Name: "tags", SQLType: tags, NonNullElements: new(true)},
	)
	engine := &fakedialect.Engine{NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, name)
	}}
	analyser := NewQueryAnalyser(engine, catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
	analyser.SetElementNullability(resolve.NewElementNullability(func(sqlmodel.SQLType) bool { return true }, false, nil))
	return analyser
}

func TestApplyQueryColumnOverridesAppliesEachDirectiveToItsColumn(t *testing.T) {
	t.Parallel()

	parsed := sqlmodel.GoType{Package: "example.com/ids", Name: "ID"}
	testCases := []struct {
		override       *directive.ColumnOverride
		check          func(t *testing.T, column sqlmodel.OutputColumn)
		name           string
		wantDiagnostic string
	}{
		{name: "a type, matched regardless of case", override: &directive.ColumnOverride{Name: "ID", SQLType: "uuid"},
			check: func(t *testing.T, column sqlmodel.OutputColumn) {
				t.Helper()
				require.Equal(t, sqlmodel.TypeCategoryUUID, column.SQLType.Category)
			}},
		{name: "nullability", override: &directive.ColumnOverride{Name: "id", Nullable: new(true)},
			check: func(t *testing.T, column sqlmodel.OutputColumn) {
				t.Helper()
				require.True(t, column.Nullable)
			}},
		{name: "a go_type the directive parser read", override: &directive.ColumnOverride{Name: "id", GoType: "example.com/ids.ID", ParsedGoType: &parsed},
			check: func(t *testing.T, column sqlmodel.OutputColumn) {
				t.Helper()
				require.Equal(t, &parsed, column.GoTypeOverride)
				require.NotSame(t, &parsed, column.GoTypeOverride, "the column holds its own copy")
			}},
		{name: "a go_type the directive parser could not read", override: &directive.ColumnOverride{Name: "id", GoType: "not a type"},
			check: func(t *testing.T, column sqlmodel.OutputColumn) {
				t.Helper()
				require.Nil(t, column.GoTypeOverride)
			}},
		{name: "struct tags", override: &directive.ColumnOverride{Name: "id", StructTags: map[string]string{"json": "id"}},
			check: func(t *testing.T, column sqlmodel.OutputColumn) {
				t.Helper()
				require.Equal(t, map[string]string{"json": "id"}, column.StructTags)
			}},
		{name: "a column the query does not return", override: &directive.ColumnOverride{Name: "idd", SQLType: "uuid"},
			wantDiagnostic: "Q036",
			check: func(t *testing.T, column sqlmodel.OutputColumn) {
				t.Helper()
				require.Equal(t, sqlmodel.TypeCategoryInteger, column.SQLType.Category)
			}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analyser := helperAnalyser(t)
			var diagnostics []diag.SourceError
			columns := analyser.applyQueryColumnOverrides([]sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
			}, &directive.DirectiveBlock{ColumnOverrides: []*directive.ColumnOverride{testCase.override}}, "users.sql", &diagnostics)

			testCase.check(t, columns[0])
			if testCase.wantDiagnostic == "" {
				require.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			require.Equal(t, testCase.wantDiagnostic, diagnostics[0].Code)
			require.Equal(t, "id", diagnostics[0].Suggestion)
		})
	}
}

func TestApplyElementNullabilityFollowsTheColumnTheCatalogueAndTheAnalysis(t *testing.T) {
	t.Parallel()

	tags := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "text[]")
	tags.ElementType = new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"))
	numbers := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "int4[]")
	numbers.ElementType = new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4"))
	testCases := []struct {
		column        sqlmodel.OutputColumn
		overrides     []*directive.ColumnOverride
		name          string
		elementsKnown []bool
		wantNullable  bool
	}{
		{name: "the preset's choice for a computed column", column: sqlmodel.OutputColumn{Name: "labels", SQLType: tags}, wantNullable: true},
		{name: "the column's own directive", column: sqlmodel.OutputColumn{Name: "labels", SQLType: tags}, wantNullable: false,
			overrides: []*directive.ColumnOverride{{Name: "LABELS", NonNullElements: new(true)}}},
		{name: "the catalogue column it projects as its own type", wantNullable: false,
			column: sqlmodel.OutputColumn{Name: "tags", SQLType: tags, SourceTable: "users", SourceColumn: "tags"}},
		{name: "a catalogue column it projects as another type", wantNullable: true,
			column: sqlmodel.OutputColumn{Name: "tags", SQLType: numbers, SourceTable: "users", SourceColumn: "tags"}},
		{name: "a catalogue column the catalogue does not hold", wantNullable: true,
			column: sqlmodel.OutputColumn{Name: "tags", SQLType: tags, SourceTable: "people", SourceColumn: "tags"}},
		{name: "elements the analysis knows are never NULL", column: sqlmodel.OutputColumn{Name: "labels", SQLType: tags},
			elementsKnown: []bool{true}, wantNullable: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analyser := helperAnalyser(t)
			columns := analyser.applyElementNullability([]sqlmodel.OutputColumn{testCase.column}, testCase.elementsKnown,
				&directive.DirectiveBlock{ColumnOverrides: testCase.overrides})

			require.NotNil(t, columns[0].SQLType.ElementType)
			require.Equal(t, testCase.wantNullable, columns[0].SQLType.ElementType.Nullable)
		})
	}
}

func TestApplyElementNullabilityLeavesTheTypesAloneWithoutAPolicy(t *testing.T) {
	t.Parallel()

	tags := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "text[]")
	tags.ElementType = new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"))
	testCases := []struct {
		name   string
		policy *resolve.ElementNullability
	}{
		{name: "no policy set", policy: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analyser := helperAnalyser(t)
			analyser.SetElementNullability(testCase.policy)

			columns := analyser.applyElementNullability([]sqlmodel.OutputColumn{{Name: "labels", SQLType: tags}}, nil, &directive.DirectiveBlock{})

			require.False(t, columns[0].SQLType.ElementType.Nullable)
		})
	}
}

func TestPaginationBoundDiagnosticsReportABoundTheGeneratedIntCannotHold(t *testing.T) {
	t.Parallel()

	directiveSpan := diagbuild.TextSpan{Line: 2, Column: 4, EndLine: 2, EndColumn: 30}
	testCases := []struct {
		parameter    sqlmodel.QueryParameter
		name         string
		wantMessages []string
	}{
		{name: "bounds an int holds everywhere", wantMessages: nil,
			parameter: sqlmodel.QueryParameter{Number: 1, Context: sqlmodel.ParameterContextLimit, DefaultLimit: new(50), MaxLimit: new(maxPortableBound)}},
		{name: "a default past what a 32-bit int holds",
			parameter: sqlmodel.QueryParameter{Number: 1, Context: sqlmodel.ParameterContextLimit, DefaultLimit: new(maxPortableBound + 1)},
			wantMessages: []string{"sgl.param: default 2147483648 does not fit the int field the generated code gives a LIMIT or OFFSET " +
				"parameter, which holds at most 2147483647 on 32-bit platforms; lower default"}},
		{name: "a max past what a 32-bit int holds, on an OFFSET",
			parameter: sqlmodel.QueryParameter{Number: 1, Context: sqlmodel.ParameterContextOffset, MaxLimit: new(maxPortableBound + 5)},
			wantMessages: []string{"sgl.param: max 2147483652 does not fit the int field the generated code gives a LIMIT or OFFSET " +
				"parameter, which holds at most 2147483647 on 32-bit platforms; lower max"}},
		{name: "a parameter that binds no LIMIT or OFFSET", wantMessages: nil,
			parameter: sqlmodel.QueryParameter{Number: 1, Context: sqlmodel.ParameterContextComparison, DefaultLimit: new(maxPortableBound + 1)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analyser := helperAnalyser(t)
			location := statementLocation{filename: "users.sql", startLine: 1}

			diagnostics := analyser.paginationBoundDiagnostics([]sqlmodel.QueryParameter{testCase.parameter},
				[]*directive.ParameterDirective{{Number: 1, Name: "page_size", Span: directiveSpan}}, location)

			messages := make([]string, 0, len(diagnostics))
			for _, diagnostic := range diagnostics {
				require.Equal(t, diag.CodeInvalidKeywordArgumentValue, diagnostic.Code)
				require.Equal(t, diag.SeverityError, diagnostic.Severity)
				require.Equal(t, directiveSpan.Line, diagnostic.Line)
				require.Equal(t, directiveSpan.Column, diagnostic.Column)
				messages = append(messages, diagnostic.Message)
			}
			if testCase.wantMessages == nil {
				require.Empty(t, messages)
				return
			}
			require.Equal(t, testCase.wantMessages, messages)
		})
	}
}
