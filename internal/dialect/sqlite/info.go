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
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// Family is the family of SQLite: the name Info.Family reports, which tools such as the
	// sgl command key their own choices on, such as which migration backend to use.
	Family = "sqlite"

	// driverModernc is the module of the pure-Go SQLite driver.
	driverModernc = "modernc.org/sqlite"

	// driverMattn is the module of the cgo SQLite driver.
	driverMattn = "github.com/mattn/go-sqlite3"

	// maxParameters is the most parameters one statement binds under the default
	// SQLITE_MAX_VARIABLE_NUMBER of builds before 3.32, used here as a conservative default.
	maxParameters = 999
)

var (
	// schemaSearchPath is where SQLite looks for an unqualified name: the main schema, then
	// the temp schema.
	schemaSearchPath = []string{"main", "temp"}

	// drivers are the modules of the Go drivers generated code may run SQLite through.
	drivers = []string{driverModernc, driverMattn}

	// placeholders is how SQLite writes a parameter: ?, ?N, :name, @name or $name, with the
	// statement executed with ? and ?N.
	placeholders = dialect.Placeholders{
		Sigils: []dialect.PlaceholderSigil{
			{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
			{Prefix: ":", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
			{Prefix: "@", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
			{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
		},
		Execution:  dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
		Positional: dialect.PlaceholderSigil{},
		NamePrefix: "",
	}

	// jsonPathFunctions are the functions that read a path from a JSON column that a runtime
	// predicate's column may be written with.
	jsonPathFunctions = []string{"json_extract"}

	// castForms are the conversions a runtime predicate's column may be written with:
	// CAST(column AS type), the only one SQLite has.
	castForms = []dialect.CastForm{{Before: "CAST(", Middle: " AS ", After: ")"}}

	// selectAliases lists the clauses that can name a select-list alias, each also as an
	// operand of an expression, as in WHERE name LIKE ?: SQLite resolves a name no column of
	// the clause's relations has to the select item of that alias.
	selectAliases = ast.ClauseWhere | ast.ClauseGroupBy | ast.ClauseHaving | ast.ClauseOrderBy

	// castTypes are the types a runtime predicate's column may be converted to: the names of
	// SQLite's storage classes and of the numeric affinity.
	castTypes = []string{"INTEGER", "REAL", "NUMERIC", "TEXT", "BLOB"}

	// listSyntax is how SQLite writes membership in a caller's list: IN and NOT IN a
	// subquery with no rows for an empty list. SQLite has no array parameters.
	listSyntax = dialect.ListSyntax{
		EmptyMembership: "IN (SELECT NULL WHERE 0)",
		EmptyExclusion:  "NOT IN (SELECT NULL WHERE 0)",
		ArrayMembership: dialect.Affix{},
		ArrayExclusion:  dialect.Affix{},
	}

	// paginationSyntax is how SQLite writes a row limit and an offset: LIMIT and OFFSET,
	// where an offset needs a limit before it, so a lone offset follows a negative limit,
	// which SQLite reads as no limit at all.
	paginationSyntax = dialect.PaginationSyntax{
		Limit:          dialect.Affix{Before: "LIMIT ", After: ""},
		Offset:         dialect.Affix{Before: "OFFSET ", After: ""},
		UnboundedLimit: "-1",
		OffsetFirst:    false,
	}
)
