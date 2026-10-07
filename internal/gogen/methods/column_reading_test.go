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
