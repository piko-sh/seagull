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

package cockroachdb

import (
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

// recordTypeName is the type CockroachDB gives the row of any relation used as a value.
const recordTypeName = "record"

// rowValueType types a relation's current row using CockroachDB's rules.
//
// Rows are non-null records, including unmatched outer-join rows containing NULL fields.
// A single-column function alias resolves to its column first.
//
// Returns *sqlmodel.SQLType which is the row's type.
func rowValueType(_ dialect.RowRelation) *sqlmodel.SQLType {
	row := sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, recordTypeName)
	row.Nullable = false
	return &row
}
