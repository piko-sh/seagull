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

package methods

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

type arrayJSONReader struct{}

func (arrayJSONReader) ColumnReading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryArray {
		return gogen.ColumnReadAsJSON
	}
	return gogen.ColumnReadAsIs
}

func TestCheckColumnsReadableRefusesAJSONColumnTheSQLCannotWrap(t *testing.T) {
	t.Parallel()

	tags := sqlmodel.OutputColumn{Name: "tags", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "text[]", ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}}
	testCases := []struct {
		name     string
		template sqlmodel.SQLTemplate
		want     string
	}{
		{name: "a column the projection wraps", template: template(
			sqlmodel.SQLSegment{Kind: sqlmodel.SegmentColumn, Column: 0, Children: []sqlmodel.SQLSegment{text("tags")}},
			text(" FROM boats")), want: ""},
		{name: "a column the SQL cannot wrap", template: template(text("SELECT tags FROM boats UNION SELECT tags FROM ports")),
			want: `query "ListTags": column "tags" column cannot be auto-decoded in this query shape`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			driver := testDriver()
			driver.Reader = arrayJSONReader{}
			info := testDialectInfo(dollar, dialect.PlaceholderSigil{})
			info.ToJSON = "to_json"
			emission := testEmission(info, textMappings(), driver)
			query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
				Name: "ListTags", Filename: "boats.sql", Command: sqlmodel.QueryCommandMany, ReadOnly: true,
				OutputColumns: []sqlmodel.OutputColumn{tags}, Template: testCase.template,
			})

			err := CheckColumnsReadable(emission, query)
			if testCase.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errUnwrappableColumn)
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

type decimalTextReader struct{}

func (decimalTextReader) ColumnReading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryArray {
		return gogen.ColumnReadAsJSON
	}
	if sqlType.Category == sqlmodel.TypeCategoryDecimal || sqlType.Temporal == sqlmodel.TemporalKindInterval {
		return gogen.ColumnReadAsText
	}
	return gogen.ColumnReadAsIs
}

type convertingTextReader struct{ decimalTextReader }

func (convertingTextReader) ConvertingScan(destination ast.Expr) ast.Expr { return destination }

func TestCheckRewrittenShapesRefusesClausesThatWouldUseTheRewrite(t *testing.T) {
	t.Parallel()

	amount := sqlmodel.OutputColumn{Name: "amount", SourceTable: "prices", SourceColumn: "amount", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryDecimal, EngineName: "decimal"}}
	wait := sqlmodel.OutputColumn{Name: "wait", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryTemporal, Temporal: sqlmodel.TemporalKindInterval, EngineName: "interval"}}
	tags := sqlmodel.OutputColumn{Name: "tags", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "text[]", ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}}
	plain := sqlmodel.OutputColumn{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"}}
	testCases := []struct {
		name       string
		columns    []sqlmodel.OutputColumn
		shape      sqlmodel.ResultShape
		want       string
		converting bool
	}{
		{name: "an order by a rewritten column's output name", columns: []sqlmodel.OutputColumn{plain, amount}, shape: sqlmodel.ResultShape{Named: []int{1}}, want: "ORDER BY prices.amount"},
		{name: "an order by position", columns: []sqlmodel.OutputColumn{amount}, shape: sqlmodel.ResultShape{Ordinal: true}, want: "position"},
		{name: "DISTINCT over an interval read as text", columns: []sqlmodel.OutputColumn{wait}, shape: sqlmodel.ResultShape{Distinct: true}, want: "DISTINCT"},
		{name: "DISTINCT over an array read as JSON", columns: []sqlmodel.OutputColumn{tags}, shape: sqlmodel.ResultShape{Distinct: true}, want: "DISTINCT"},
		{name: "a compound statement", columns: []sqlmodel.OutputColumn{amount}, shape: sqlmodel.ResultShape{Compound: true}, want: "UNION"},
		{name: "DISTINCT over a decimal read as text at its fixed scale", columns: []sqlmodel.OutputColumn{amount}, shape: sqlmodel.ResultShape{Distinct: true}},
		{name: "an order by the name of a column read as it is", columns: []sqlmodel.OutputColumn{plain, amount}, shape: sqlmodel.ResultShape{Named: []int{0}}},
		{name: "a query that rewrites nothing", columns: []sqlmodel.OutputColumn{plain}, shape: sqlmodel.ResultShape{Distinct: true, Compound: true, Ordinal: true, Named: []int{0}}},
		{name: "a driver that converts its own value reads the column unwrapped", columns: []sqlmodel.OutputColumn{wait}, shape: sqlmodel.ResultShape{Distinct: true, Ordinal: true}, converting: true},
		{name: "a converting driver still cannot read an array without JSON", columns: []sqlmodel.OutputColumn{tags}, shape: sqlmodel.ResultShape{Distinct: true}, converting: true, want: "DISTINCT"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			driver := testDriver()
			driver.Reader = decimalTextReader{}
			if testCase.converting {
				driver.Reader = convertingTextReader{}
			}
			info := testDialectInfo(dollar, dialect.PlaceholderSigil{})
			info.ToJSON = "to_json"
			info.TextType = "text"
			emission := testEmission(info, textMappings(), driver)
			query := &sqlmodel.AnalysedQuery{
				Name: "ListPrices", Filename: "prices.sql", Command: sqlmodel.QueryCommandMany, ReadOnly: true,
				OutputColumns: testCase.columns, ResultShape: testCase.shape,
			}

			problems := CheckRewrittenShapes(emission, query)
			if testCase.want == "" {
				require.Empty(t, problems)
				return
			}
			require.Len(t, problems, 1)
			require.Same(t, query, problems[0].Query)
			require.Contains(t, problems[0].Message, testCase.want)
		})
	}
}
