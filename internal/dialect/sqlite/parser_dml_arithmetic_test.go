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

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestADivisionByAnythingButANonZeroConstantMayBeNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		mayBeNull  bool
	}{
		{name: "a column divisor", expression: "id / user_id", mayBeNull: true},
		{name: "a column modulus", expression: "id % user_id", mayBeNull: true},
		{name: "a constant divisor", expression: "id / 2", mayBeNull: false},
		{name: "a negative constant divisor", expression: "id / -2", mayBeNull: false},
		{name: "a fractional divisor", expression: "id / 0.5", mayBeNull: false},
		{name: "a hexadecimal divisor", expression: "id % 0x10", mayBeNull: false},
		{name: "a zero divisor", expression: "id / 0", mayBeNull: true},
		{name: "a zero real divisor", expression: "id / 0.0", mayBeNull: true},
		{name: "a modulus below one, which SQLite takes as zero", expression: "id % 0.5", mayBeNull: true},
		{name: "a modulus of one and a half", expression: "id % 1.5", mayBeNull: false},
		{name: "a text divisor", expression: "id / '2'", mayBeNull: true},
		{name: "a divisor in parentheses", expression: "id / (user_id + 1)", mayBeNull: true},
		{name: "a product", expression: "id * user_id", mayBeNull: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), "SELECT "+test.expression+" AS x FROM posts")
			require.Len(t, analysis.OutputColumns, 1)
			operation, isOperation := analysis.OutputColumns[0].Expression.(*ast.BinaryOpExpression)
			require.True(t, isOperation, "%T", analysis.OutputColumns[0].Expression)
			require.Equal(t, test.mayBeNull, operation.MayBeNull)
		})
	}
}
