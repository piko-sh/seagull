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

package gotypes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/sqlmodel"
)

func nestedArray(depth int) sqlmodel.SQLType {
	sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	for range depth {
		element := sqlType
		sqlType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "array")
		sqlType.ElementType = &element
	}
	return sqlType
}

func TestTypeTooDeep(t *testing.T) {
	t.Parallel()

	shallow := nestedArray(3)
	deep := nestedArray(sqlmodel.MaxTypeDepth + 1)
	inStruct := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	inStruct.StructFields = []sqlmodel.StructField{{Name: "f", SQLType: deep}}

	assert.False(t, TypeTooDeep(&shallow))
	assert.True(t, TypeTooDeep(&deep))
	assert.True(t, TypeTooDeep(&inStruct))
	assert.False(t, TypeTooDeep(nil))
}

func TestOverrideTooDeep(t *testing.T) {
	t.Parallel()

	assert.False(t, OverrideTooDeep(sqlmodel.TypeOverride{SQLTypeName: "text[][]"}))
	assert.True(t, OverrideTooDeep(sqlmodel.TypeOverride{SQLTypeName: "text" + strings.Repeat("[]", sqlmodel.MaxArrayDimensions+1)}))
}
