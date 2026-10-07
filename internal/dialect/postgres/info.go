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

import "seagull.sh/seagull/internal/dialect"

const (
	// Family is the family of PostgreSQL, and of the dialects built on it, such as
	// CockroachDB and TimescaleDB: the name Info.Family reports, which tools such as the sgl
	// command key their own choices on, such as which migration backend to use.
	Family = "postgres"

	// driverPgx is the module of the pgx driver, which the database/sql emitter reaches
	// through its stdlib package and the pgx emitter uses natively.
	driverPgx = "github.com/jackc/pgx/v5"

	// maxParameters is the most parameters one statement binds, set by the 16-bit parameter
	// count of the wire protocol.
	maxParameters = 65535
)

var (
	// filterOperators are the comparison operators beyond the common ones a runtime
	// predicate may use: case-insensitive LIKE and the jsonb key-existence operators.
	filterOperators = []string{"ILIKE", "?", "?|", "?&"}

	// systemColumns are the columns PostgreSQL gives every table without the table declaring
	// them.
	systemColumns = []string{"tableoid", "xmin", "cmin", "xmax", "cmax", "ctid"}

	// castForms are the conversions a runtime predicate's column may be written with: the
	// standard CAST(column AS type) and PostgreSQL's (column)::type.
	castForms = []dialect.CastForm{
		{Before: "CAST(", Middle: " AS ", After: ")"},
		{Before: "(", Middle: ")::", After: ""},
	}

	// castTypes are the types a runtime predicate's column may be converted to.
	castTypes = []string{"boolean", "bool", "smallint", "integer", "int", "bigint", "numeric", "real", "double precision", "text"}

	// listSyntax describes PostgreSQL membership tests using array parameters and literals.
	//
	// Empty tests use array literals so the tested value supplies their element type.
	listSyntax = dialect.ListSyntax{
		EmptyMembership: "= ANY('{}')",
		EmptyExclusion:  "<> ALL('{}')",
		ArrayMembership: dialect.Affix{Before: "= ANY(", After: ")"},
		ArrayExclusion:  dialect.Affix{Before: "<> ALL(", After: ")"},
	}

	// paginationSyntax is how PostgreSQL writes a row limit and an offset: LIMIT and OFFSET,
	// each of which may stand alone.
	paginationSyntax = dialect.PaginationSyntax{
		Limit:          dialect.Affix{Before: "LIMIT ", After: ""},
		Offset:         dialect.Affix{Before: "OFFSET ", After: ""},
		UnboundedLimit: "",
		OffsetFirst:    false,
	}
)
