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

package mysql

import "seagull.sh/seagull/internal/dialect"

const (
	// Family is the family of MySQL, and of the flavours built on it, such as MariaDB: the
	// name Info.Family reports, which tools such as the sgl command key their own choices
	// on, such as which migration backend to use.
	Family = "mysql"

	// driverMySQL is the module of the go-sql-driver MySQL driver.
	driverMySQL = "github.com/go-sql-driver/mysql"

	// maxParameters is the most parameters one prepared statement binds, set by the 16-bit
	// parameter count of the wire protocol.
	maxParameters = 65535
)

var (
	// jsonPathFunctions are the functions that read a path from a JSON column that a runtime
	// predicate's column may be written with.
	jsonPathFunctions = []string{"json_extract"}

	// systemColumns are the columns MySQL lets a query name on a table without the table
	// declaring them: _rowid, the alias of a single-column integer primary key.
	systemColumns = []string{"_rowid"}

	// castForms are the conversions a runtime predicate's column may be written with:
	// CAST(column AS type), the only one MySQL has.
	castForms = []dialect.CastForm{{Before: "CAST(", Middle: " AS ", After: ")"}}

	// castTypes are the types a runtime predicate's column may be converted to: the target
	// types CAST accepts, which are not the column types MySQL declares.
	castTypes = []string{"SIGNED", "UNSIGNED", "DECIMAL", "DOUBLE", "CHAR", "DATE", "DATETIME"}

	// listSyntax is how MySQL writes membership in a caller's list: IN and NOT IN a subquery
	// with no rows, which needs FROM DUAL to carry a WHERE clause, for an empty list. MySQL
	// has no array parameters.
	listSyntax = dialect.ListSyntax{
		EmptyMembership: "IN (SELECT NULL FROM DUAL WHERE FALSE)",
		EmptyExclusion:  "NOT IN (SELECT NULL FROM DUAL WHERE FALSE)",
		ArrayMembership: dialect.Affix{},
		ArrayExclusion:  dialect.Affix{},
	}

	// placeholderForms describes the anonymous, numbered and named parameter placeholders
	// MySQL reads.
	//
	// These are ?, ?N and :name; execution uses the anonymous form. The value is read, never
	// changed.
	placeholderForms = dialect.Placeholders{
		Sigils: []dialect.PlaceholderSigil{
			{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous},
			{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
			{Prefix: ":", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
		},
		Execution:  dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous},
		Positional: dialect.PlaceholderSigil{},
		NamePrefix: "",
	}

	// paginationSyntax is how MySQL writes a row limit and an offset: LIMIT and OFFSET,
	// where an offset needs a limit before it, so a lone offset follows the largest limit
	// MySQL accepts.
	paginationSyntax = dialect.PaginationSyntax{
		Limit:          dialect.Affix{Before: "LIMIT ", After: ""},
		Offset:         dialect.Affix{Before: "OFFSET ", After: ""},
		UnboundedLimit: "18446744073709551615",
		OffsetFirst:    false,
	}
)
