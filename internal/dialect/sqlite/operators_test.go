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
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestIsComparisonsAreNullTests(t *testing.T) {
	t.Parallel()

	features := New().Info().OperatorFeatures
	for _, operator := range []string{"IS", "IS NOT"} {
		require.Equal(t, ast.SQLFeatureIsNull, features[operator], operator)
	}
}

func TestNotIsNullOnANullOperand(t *testing.T) {
	t.Parallel()

	_, behaviour, typed := New().UnaryResult("NOT", sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "boolean"))

	require.True(t, typed)
	require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)
}
