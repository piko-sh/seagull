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

package mysql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestKeywordFirstArgumentIsTextNotAColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		call        string
		wantKeyword bool
	}{
		{name: "the unit of TIMESTAMPDIFF", call: "TIMESTAMPDIFF(HOUR, created_at, updated_at)", wantKeyword: true},
		{name: "the unit of TIMESTAMPADD in lower case", call: "timestampadd(minute, 1, created_at)", wantKeyword: true},
		{name: "an SQL_TSI_ unit", call: "TIMESTAMPDIFF(SQL_TSI_DAY, created_at, updated_at)", wantKeyword: true},
		{name: "the type of GET_FORMAT", call: "GET_FORMAT(DATE, 'EUR')", wantKeyword: true},
		{name: "a column that names no unit", call: "TIMESTAMPDIFF(unit, created_at, updated_at)", wantKeyword: false},
		{name: "a function that takes no keyword", call: "DATEDIFF(HOUR, created_at)", wantKeyword: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, "SELECT "+testCase.call+" AS v FROM events")

			require.Len(t, analysis.OutputColumns, 1)
			call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
			require.True(t, isCall)
			_, isLiteral := call.Arguments[0].(*ast.LiteralExpression)
			require.Equal(t, testCase.wantKeyword, isLiteral)
		})
	}
}
