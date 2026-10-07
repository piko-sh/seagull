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

package emitkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func nestedArray(levels int) sqlmodel.SQLType {
	sqlType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	for range levels {
		sqlType = sqlmodel.SQLType{EngineName: "Array", Category: sqlmodel.TypeCategoryArray, ElementType: new(sqlType)}
	}
	return sqlType
}

func TestCheckTypeDepthRefusesAValueWhoseTypeNestsPastTheLimit(t *testing.T) {
	t.Parallel()

	tooDeep := nestedArray(sqlmodel.MaxTypeDepth)
	deepest := nestedArray(sqlmodel.MaxTypeDepth - 1)
	message := func(subject string) string {
		return subject + " has a type nesting 64 or more levels of elements, keys, fields or members, deeper than " +
			"generated code follows; flatten the type"
	}

	testCases := []struct {
		name      string
		parameter sqlmodel.SQLType
		column    sqlmodel.SQLType
		table     sqlmodel.SQLType
		want      []string
		atQuery   []bool
	}{
		{name: "types up to the limit pass", parameter: deepest, column: deepest, table: deepest},
		{
			name: "a parameter past the limit", parameter: tooDeep, column: deepest, table: deepest,
			want: []string{message("parameter grid")}, atQuery: []bool{true},
		},
		{
			name: "an output column past the limit", parameter: deepest, column: tooDeep, table: deepest,
			want: []string{message("column cells")}, atQuery: []bool{true},
		},
		{
			name: "a table's column past the limit", parameter: deepest, column: deepest, table: tooDeep,
			want: []string{message("column layers of table audit.grids")}, atQuery: []bool{false},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{
				Name:          "ReadGrid",
				Parameters:    []sqlmodel.QueryParameter{{Number: 1, Name: "grid", SQLType: testCase.parameter}},
				OutputColumns: []sqlmodel.OutputColumn{{Name: "cells", SQLType: testCase.column}},
			}
			catalogue := &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{"audit": {
				Name: "audit",
				Tables: map[string]*sqlmodel.Table{"grids": {
					Schema: "audit", Name: "grids", Columns: []sqlmodel.Column{{Name: "layers", SQLType: testCase.table}},
				}},
			}}}
			problems := emitkit.CheckTypeDepth(&emit.Request{Catalogue: catalogue, Queries: []*sqlmodel.AnalysedQuery{query}})

			require.Len(t, problems, len(testCase.want))
			for index, problem := range problems {
				require.Equal(t, diag.CodeEmitterUnsupported, problem.Code)
				require.Equal(t, diag.SeverityError, problem.Severity)
				require.Equal(t, testCase.want[index], problem.Message)
				if testCase.atQuery[index] {
					require.Same(t, query, problem.Query)
					continue
				}
				require.Nil(t, problem.Query)
			}
		})
	}
}
