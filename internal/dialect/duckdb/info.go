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
	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// Family is the family of DuckDB: the name Info.Family reports, which tools such as the
	// sgl command key their own choices on, such as which migration backend to use.
	Family = "duckdb"

	// driverDuckDB is the module of the DuckDB driver.
	driverDuckDB = "github.com/duckdb/duckdb-go/v2"

	// maxParameters is the most parameters the generated code binds in one statement. DuckDB
	// sets no limit of its own, so it keeps to the common 16-bit ceiling.
	maxParameters = 65535
)

var (
	// filterOperators are the comparison operators beyond the common ones a runtime
	// predicate may use: case-insensitive LIKE.
	filterOperators = []string{"ILIKE"}

	// jsonPathFunctions are the functions that read a path from a JSON column that a runtime
	// predicate's column may be written with.
	jsonPathFunctions = []string{"json_extract"}

	// systemColumns are the columns DuckDB gives every table without the table declaring
	// them.
	systemColumns = []string{"rowid"}

	// selectAliases lists clauses and select items that can reference DuckDB select aliases.
	//
	// Earlier select aliases are visible to later items. Relation columns win over aliases
	// with the same name.
	selectAliases = ast.ClauseWhere | ast.ClauseGroupBy | ast.ClauseHaving | ast.ClauseRowShaping | ast.ClauseOrderBy |
		ast.ClauseSelectList

	// selectAliasOperands lists the clauses in which a select-list alias may also be an
	// operand of an expression, as in WHERE total > ?: every clause that can name one except
	// GROUP BY, where DuckDB takes an alias only as a whole grouping key.
	selectAliasOperands = ast.ClauseWhere | ast.ClauseHaving | ast.ClauseRowShaping | ast.ClauseOrderBy

	// castForms are the conversions a runtime predicate's column may be written with: the
	// standard CAST(column AS type) and DuckDB's (column)::type.
	castForms = []dialect.CastForm{
		{Before: "CAST(", Middle: " AS ", After: ")"},
		{Before: "(", Middle: ")::", After: ""},
	}

	// castTypes are the types a runtime predicate's column may be converted to.
	castTypes = []string{"BOOLEAN", "BOOL", "SMALLINT", "INTEGER", "INT", "BIGINT", "DECIMAL", "NUMERIC", "REAL", "DOUBLE", "VARCHAR", "TEXT", "BLOB"}

	// listSyntax is how DuckDB writes membership in a caller's list: IN and NOT IN a
	// subquery with no rows for an empty list.
	listSyntax = dialect.ListSyntax{
		EmptyMembership: "IN (SELECT NULL WHERE FALSE)",
		EmptyExclusion:  "NOT IN (SELECT NULL WHERE FALSE)",
		ArrayMembership: dialect.Affix{},
		ArrayExclusion:  dialect.Affix{},
	}

	// paginationSyntax is how DuckDB writes a row limit and an offset: LIMIT and OFFSET,
	// each of which may stand alone.
	paginationSyntax = dialect.PaginationSyntax{
		Limit:          dialect.Affix{Before: "LIMIT ", After: ""},
		Offset:         dialect.Affix{Before: "OFFSET ", After: ""},
		UnboundedLimit: "",
		OffsetFirst:    false,
	}
)
