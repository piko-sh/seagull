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

package postgres

import (
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// anonymousRowType is the type PostgreSQL gives the row of a relation that no table or
// view names, such as a derived table or a common table expression.
const anonymousRowType = "record"

// RowValueType types a relation's current row using PostgreSQL's rules and configured
// hooks.
//
// Tables and views use their composite type; single-value functions use their value type;
// other relations use record. Unmatched outer-join rows are NULL. Configured hooks answer
// first.
//
// Takes relation (dialect.RowRelation) which describes the relation.
//
// Returns sqlmodel.SQLType which is the row's type.
func (engine *Dialect) RowValueType(relation dialect.RowRelation) sqlmodel.SQLType {
	hooked, answered := dialectkit.FirstHookAnswer(engine.settings.RowValueTypeHooks,
		func(hook func(relation dialect.RowRelation) *sqlmodel.SQLType) *sqlmodel.SQLType {
			return hook(relation)
		})
	if answered {
		return hooked
	}
	if relation.Function && len(relation.Columns) == 1 {
		value := relation.Columns[0].SQLType
		value.Nullable = value.Nullable || relation.Columns[0].Nullable || relation.OuterJoined
		return value
	}
	row := sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, anonymousRowType)
	if relation.Name != "" {
		row.EngineName, row.Schema = relation.Name, relation.Schema
	}
	row.Nullable = relation.OuterJoined
	return row
}
