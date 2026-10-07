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
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestArgumentTypesMatch(t *testing.T) {
	t.Parallel()

	text := sqlmodel.FunctionArgument{Name: "a", Type: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")}
	varchar := sqlmodel.FunctionArgument{Name: "b", Type: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")}
	integer := sqlmodel.FunctionArgument{Name: "c", Type: sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "text")}

	testCases := []struct {
		name  string
		left  []sqlmodel.FunctionArgument
		right []sqlmodel.FunctionArgument
		want  bool
	}{
		{name: "both empty", want: true},
		{name: "same types different names", left: []sqlmodel.FunctionArgument{text}, right: []sqlmodel.FunctionArgument{{Name: "z", Type: text.Type}}, want: true},
		{name: "different lengths", left: []sqlmodel.FunctionArgument{text}, right: []sqlmodel.FunctionArgument{text, text}},
		{name: "different engine names", left: []sqlmodel.FunctionArgument{text}, right: []sqlmodel.FunctionArgument{varchar}},
		{name: "different categories", left: []sqlmodel.FunctionArgument{text}, right: []sqlmodel.FunctionArgument{integer}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, ArgumentTypesMatch(testCase.left, testCase.right))
		})
	}
}

func TestNewOutputColumn(t *testing.T) {
	t.Parallel()

	sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")

	column := NewOutputColumn("email", sqlType, true)

	want := sqlmodel.OutputColumn{}
	want.Name = "email"
	want.SQLType = sqlType
	want.Nullable = true
	assert.Equal(t, want, column)
}
