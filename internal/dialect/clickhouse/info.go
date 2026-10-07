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

package clickhouse

import (
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// Family is the family of ClickHouse: the name Info.Family reports, which tools such as
	// the sgl command key their own choices on, such as which migration backend to use.
	Family = "clickhouse"

	// boundNamePrefix is prepended to the name of every parameter the generated code binds,
	// in the {name:Type} placeholder and in the binding alike.
	//
	// ClickHouse rejects a server-side parameter whose name is a reserved keyword, so a
	// query with `LIMIT {limit:Int32}` fails to parse on the server. Prefixing every name,
	// not only the ones that collide, keeps the rewrite injective so two parameters can
	// never alias, and needs no reserved-word list to maintain.
	boundNamePrefix = "p_"

	// driverClickHouse is the module of the clickhouse-go driver.
	driverClickHouse = "github.com/ClickHouse/clickhouse-go/v2"

	// maxParameters is the most parameters the generated code binds in one statement.
	// ClickHouse documents no hard cap, but the driver writes a 16-bit length while binding.
	maxParameters = 65535
)

var (
	// bracedPlaceholder is ClickHouse's only parameter form, {name:Type}: the server binds
	// it by name and parses the value as the type the placeholder names.
	bracedPlaceholder = dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed}

	// driverPositionalPlaceholder is the numbered form clickhouse-go also binds, by
	// substituting each value into the query on the client, which the generated code writes
	// where it must bind by position, such as a runtime query builder's predicates. The
	// server itself binds only the braced form.
	driverPositionalPlaceholder = dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}

	// filterOperators are the comparison operators beyond the common ones a runtime
	// predicate may use: case-insensitive LIKE.
	filterOperators = []string{"ILIKE"}

	// systemColumns are the virtual columns ClickHouse gives a table without the table
	// declaring them: those of the MergeTree family, those a Distributed or Merge table
	// adds, and those of the file-reading engines.
	systemColumns = []string{
		"_part", "_part_index", "_part_uuid", "_partition_id", "_partition_value", "_part_offset",
		"_part_starting_offset", "_part_granule_offset", "_part_data_version", "_sample_factor", "_row_exists",
		"_block_number", "_block_offset", "_disk_name", "_table", "_database", "_shard_num",
		"_path", "_file", "_size", "_time",
	}

	// selectAliases lists clauses where ClickHouse allows select-list aliases.
	//
	// Earlier select items can be referenced by later items; forward references are not
	// modelled.
	selectAliases = ast.ClauseWhere | ast.ClauseJoinCondition | ast.ClauseGroupBy | ast.ClauseHaving |
		ast.ClauseRowShaping | ast.ClauseOrderBy | ast.ClauseSelectList

	// castForms are the conversions a runtime predicate's column may be written with: the
	// standard CAST(column AS type) and ClickHouse's (column)::type.
	castForms = []dialect.CastForm{
		{Before: "CAST(", Middle: " AS ", After: ")"},
		{Before: "(", Middle: ")::", After: ""},
	}

	// castTypes are the types a runtime predicate's column may be converted to: the standard
	// names ClickHouse reads without regard to case, since its own type names, such as
	// Int64, are case-sensitive.
	castTypes = []string{"BOOLEAN", "SMALLINT", "INTEGER", "INT", "BIGINT", "REAL", "DOUBLE", "TEXT"}

	// listSyntax is how ClickHouse writes membership in a caller's list: IN and NOT IN a
	// subquery with no rows for an empty list.
	listSyntax = dialect.ListSyntax{
		EmptyMembership: "IN (SELECT NULL WHERE 0)",
		EmptyExclusion:  "NOT IN (SELECT NULL WHERE 0)",
		ArrayMembership: dialect.Affix{},
		ArrayExclusion:  dialect.Affix{},
	}

	// paginationSyntax is how ClickHouse writes a row limit and an offset: LIMIT and OFFSET,
	// each of which may stand alone.
	paginationSyntax = dialect.PaginationSyntax{
		Limit:          dialect.Affix{Before: "LIMIT ", After: ""},
		Offset:         dialect.Affix{Before: "OFFSET ", After: ""},
		UnboundedLimit: "",
		OffsetFirst:    false,
	}
)

// placeholders describes ClickHouse's parameter forms: the braced {name:Type} form the
// server binds, written in the generated SQL with every name prefixed, and the numbered
// form the driver binds by position.
//
// Returns dialect.Placeholders which holds a sigil list of its own.
func placeholders() dialect.Placeholders {
	return dialect.Placeholders{
		Sigils:     []dialect.PlaceholderSigil{bracedPlaceholder},
		Execution:  bracedPlaceholder,
		Positional: driverPositionalPlaceholder,
		NamePrefix: boundNamePrefix,
	}
}
