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

import "seagull.sh/seagull/internal/dialect/ast"

// DerivedTableSource identifies the origin of a derived (virtual) table in the FROM
// clause.
type DerivedTableSource uint8

const (
	// DerivedSourceUnnest is a table produced by UNNEST(array_column).
	DerivedSourceUnnest DerivedTableSource = iota

	// DerivedSourceFlatten is a table produced by FLATTEN (BigQuery, Snowflake).
	DerivedSourceFlatten

	// DerivedSourceTableFunction is a table produced by a table-valued function.
	DerivedSourceTableFunction

	// DerivedSourceSubquery is a table produced by a subquery in the FROM clause.
	DerivedSourceSubquery
)

// DerivedTableReference describes a virtual table in the FROM clause that does not exist
// in the catalogue. The scope builder registers one for each ARRAY JOIN, table-valued
// function and subquery in FROM position a dialect reports.
type DerivedTableReference struct {
	// Using describes the columns the derived table's join merges, or nil.
	Using *ast.JoinUsing

	// Alias is the table alias used in the query.
	Alias string

	// Columns holds the resolved column types of the derived table.
	Columns []ast.ScopedColumn

	// JoinKind is the join type that introduced this derived table.
	JoinKind ast.JoinKind

	// Source identifies how the derived table was produced.
	Source DerivedTableSource
}
