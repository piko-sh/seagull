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

package dialecttest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/sqlite"
)

type aliasOperandsDialect struct {
	*sqlite.Dialect
	selectAliases ast.Clause
	operands      ast.Clause
}

func (engine aliasOperandsDialect) Info() dialect.Info {
	info := engine.Dialect.Info()
	info.SelectAliases, info.SelectAliasOperands = engine.selectAliases, engine.operands
	return info
}

func TestSelectAliasOperandsCheckHoldsTheDialectToItsInfo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		selectAliases  ast.Clause
		operands       ast.Clause
		expectSkip     bool
		expectProblems bool
	}{
		{
			name:          "a dialect listing no clause is not checked",
			selectAliases: ast.ClauseWhere | ast.ClauseHaving,
			operands:      0,
			expectSkip:    true,
		},
		{
			name:          "parameters compared with aliases in WHERE and HAVING take the aliased items' types",
			selectAliases: ast.ClauseWhere | ast.ClauseGroupBy | ast.ClauseHaving | ast.ClauseOrderBy,
			operands:      ast.ClauseWhere | ast.ClauseHaving,
		},
		{
			name:           "a clause the select-list aliases do not reach makes no alias an operand",
			selectAliases:  ast.ClauseOrderBy,
			operands:       ast.ClauseWhere,
			expectProblems: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := &Fixture{
				NewDialect: func() dialect.DialectPort {
					return aliasOperandsDialect{Dialect: sqlite.New(), selectAliases: testCase.selectAliases, operands: testCase.operands}
				},
				BaseMigration: "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);",
			}
			outcome := checkSelectAliasOperandsTypeParameters(t.Context(), checkEnvironment{testName: t.Name(), inChild: false}, fixture)

			require.Equal(t, testCase.expectSkip, outcome.NotApplicable != "")
			require.Equal(t, testCase.expectProblems, len(outcome.Problems) > 0, "problems: %v", outcome.Problems)
		})
	}
}
