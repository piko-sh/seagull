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

package dialectkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestPromoteAcrossNumericCategoriesPrefersFloatThenDecimalKeepingTheWinner(t *testing.T) {
	t.Parallel()

	smallint := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int2")
	smallint.Width = 16
	realType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "float4")
	realType.Width = 32
	numeric := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")

	cases := []struct {
		name        string
		left, right sqlmodel.SQLType
		want        sqlmodel.SQLType
		promoted    bool
	}{
		{name: "an integer meets a float", left: smallint, right: realType, want: realType, promoted: true},
		{name: "a float meets an integer", left: realType, right: smallint, want: realType, promoted: true},
		{name: "a decimal meets a float", left: numeric, right: realType, want: realType, promoted: true},
		{name: "a float meets a decimal", left: realType, right: numeric, want: realType, promoted: true},
		{name: "an integer meets a decimal", left: smallint, right: numeric, want: numeric, promoted: true},
		{name: "a decimal meets an integer", left: numeric, right: smallint, want: numeric, promoted: true},
		{name: "the same category is left to the engine", left: smallint, right: smallint, want: sqlmodel.SQLType{}, promoted: false},
		{name: "a non-number declines", left: text, right: realType, want: sqlmodel.SQLType{}, promoted: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, promoted := dialectkit.PromoteAcrossNumericCategories(testCase.left, testCase.right)
			require.Equal(t, testCase.promoted, promoted)
			require.Equal(t, testCase.want, got)
		})
	}
}
