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
	"go/ast"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

type rawReader struct{}

func (rawReader) ColumnReading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryStruct {
		return gogen.ColumnReadRaw
	}
	return gogen.ColumnReadAsIs
}

func (rawReader) RawType(sqlmodel.SQLType) sqlmodel.GoType {
	return sqlmodel.NewGoMap(sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}, sqlmodel.GoType{Composite: nil, Package: "", Name: "any"})
}

func (rawReader) ConvertRaw(destination, raw ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("sglReadNativeColumn"), destination, raw)
}

func TestRuntimeBuilderTerminalsReadAndBindEnumsThroughGoStringsForADriverThatNeedsIt(t *testing.T) {
	t.Parallel()

	enum := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryEnum, EngineName: "mood", EnumValues: []string{"calm", "stormy"}}
	driver := testDriver()
	driver.Enums = gogen.EnumStrings{Scan: gogen.EnumValue, Bind: gogen.EnumValue}
	mappings, ok := textMappings().(*gotypes.MappingTable)
	require.True(t, ok)
	mappings.Namer = func(sqlType sqlmodel.SQLType) (string, bool) {
		return "Mood", sqlType.Category == sqlmodel.TypeCategoryEnum
	}
	emission := testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), mappings, driver)
	query := runtimeBuilderQuery()
	query.Parameters[1].SQLType = enum
	query.OutputColumns = append(query.OutputColumns, sqlmodel.OutputColumn{Name: "mood", SQLType: enum, Nullable: true})

	source := renderDecls(t, BuildRuntimeBuilderDeclarations(emission, queryFor(t, emission, query)))

	require.Equal(t, 2, strings.Count(source, "args = append(args, string(builder.params.Kind))"), "the query and its count bind the enum as a string")
	require.Contains(t, source, "if err := rows.Scan(&row.ID, &row.Title, &raw.Mood); err != nil {")
	require.Contains(t, source, "err = builder.queries.reader.QueryRowContext(ctx, query, args...).Scan(&row.ID, &row.Title, &raw.Mood)")
	require.Equal(t, 2, strings.Count(source, "row.Mood = (*Mood)(raw.Mood)"), "All and One convert the raw value")
}

func TestRuntimeBuilderTerminalsConvertARawColumnAfterTheScan(t *testing.T) {
	t.Parallel()

	driver := testDriver()
	driver.Reader = rawReader{}
	emission := testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), driver)
	query := runtimeBuilderQuery()
	field := sqlmodel.StructField{Name: "label", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}
	query.OutputColumns = append(query.OutputColumns, sqlmodel.OutputColumn{
		Name: "fields", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{field}},
	})

	source := renderDecls(t, BuildRuntimeBuilderDeclarations(emission, queryFor(t, emission, query)))

	require.Contains(t, source, "if err := rows.Scan(&row.ID, &row.Title, &raw.Fields); err != nil {")
	require.Contains(t, source, "if err := sglReadNativeColumn(&row.Fields, raw.Fields); err != nil {\n\t\t\treturn nil, err\n\t\t}")
	require.Contains(t, source, "if err != nil {\n\t\treturn SearchPostsRow{}, err\n\t}\n\tif err := sglReadNativeColumn(&row.Fields, raw.Fields); err != nil {\n\t\treturn SearchPostsRow{}, err\n\t}\n\treturn row, nil")
}

func TestRuntimeBuilderOneAndCountReturnTheZeroValueWithAnError(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	built := newBuilder(emission, queryFor(t, emission, runtimeBuilderQuery()))
	testCases := []struct {
		method   *ast.FuncDecl
		name     string
		wants    []string
		unwanted string
	}{
		{
			name:     "One",
			method:   built.oneMethod(),
			wants:    []string{"if err != nil {\n\t\treturn SearchPostsRow{}, err\n\t}\n\treturn row, nil\n}"},
			unwanted: "return row, err",
		},
		{
			name:     "Count",
			method:   built.countMethod(),
			wants:    []string{"Scan(&count)\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\treturn count, nil\n}"},
			unwanted: "return count, err",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source := renderDecls(t, []ast.Decl{testCase.method})

			for _, want := range testCase.wants {
				require.Contains(t, source, want)
			}
			require.NotContains(t, source, testCase.unwanted)
		})
	}
}
