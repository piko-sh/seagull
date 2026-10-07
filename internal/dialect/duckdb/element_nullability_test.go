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

package duckdb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestListsAndMapsHoldNullElements(t *testing.T) {
	t.Parallel()

	var engine dialect.DialectPort = New()
	holder, implements := engine.(dialect.ElementNullabilityPort)
	require.True(t, implements)
	mapType := sqlmodel.SQLType{}
	mapType.Category = sqlmodel.TypeCategoryMap
	structType := sqlmodel.SQLType{}
	structType.Category = sqlmodel.TypeCategoryStruct

	for _, testCase := range []struct {
		name    string
		sqlType sqlmodel.SQLType
		holds   bool
	}{
		{name: "a list", sqlType: engine.NormaliseTypeName("INTEGER[]"), holds: true},
		{name: "a map", sqlType: mapType, holds: true},
		{name: "a struct", sqlType: structType, holds: false},
		{name: "a scalar", sqlType: engine.NormaliseTypeName("INTEGER"), holds: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.holds, holder.HoldsNullElements(testCase.sqlType))
		})
	}
}
