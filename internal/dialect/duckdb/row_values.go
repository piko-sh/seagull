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
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

// RowValueType describes a DuckDB relation's current row as a struct.
//
// The struct itself is never NULL, but its fields can be, including every field of an
// unmatched outer-join row.
//
// Takes relation (dialect.RowRelation) which describes the relation.
//
// Returns sqlmodel.SQLType which is the row's STRUCT type.
func (*Dialect) RowValueType(relation dialect.RowRelation) sqlmodel.SQLType {
	row := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	if len(relation.Columns) == 0 {
		return row
	}
	row.StructFields = make([]sqlmodel.StructField, len(relation.Columns))
	for index := range relation.Columns {
		fieldType := relation.Columns[index].SQLType
		fieldType.Nullable = true
		row.StructFields[index] = sqlmodel.StructField{Name: relation.Columns[index].Name, SQLType: fieldType, Positional: false}
	}
	return row
}
