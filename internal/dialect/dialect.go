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

package dialect

import (
	"maps"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Info describes a dialect: its identity, its comment and parameter syntax, the
// expressions it supports and the optional statement features it offers.
//
// Info is a struct rather than a set of methods so new fields can be added without
// changing every dialect.
//
// The Info a dialect returns belongs to the caller, which may keep or change it. An
// implementation builds a fresh value on each call and never hands out a slice or map it
// keeps at package level or in the dialect itself; Clone copies one.
type Info struct {
	// OperatorFeatures maps operator spellings to required expression features. Unlisted
	// binary and comparison operators use ast.SQLFeatureBinaryArithmetic and
	// ast.SQLFeatureBinaryComparison respectively; ast.SQLFeatureIsNull marks comparisons
	// that treat NULL as a value and always return a non-null result.
	OperatorFeatures map[string]ast.ExpressionFeature

	// Placeholders describes how the dialect writes query parameters: the forms a query or a
	// directive line may write one in, and the form the generated code executes.
	Placeholders Placeholders

	// IdentifierQuote is how the dialect delimits an identifier, which the generated code
	// writes when it names a column itself.
	IdentifierQuote IdentifierQuote

	// Name identifies the dialect, such as postgres or cockroachdb. A dialect built with a
	// custom name reports that name here.
	Name string

	// Family names the base dialect whose syntax this dialect shares, such as postgres.
	// Tools use it to select shared behaviour.
	Family string

	// Comments is the comment syntax of the dialect's query files: LinePrefix opens the line
	// comments directives are written in. It must open a line comment under Lexical.
	Comments ast.CommentStyle

	// TextType names the SQL type used to cast otherwise unreadable values to text. Empty
	// means the dialect provides no such type.
	TextType string

	// ToJSON names a function used to convert otherwise unreadable column values to JSON for
	// decoding. It is empty when the dialect provides none.
	ToJSON string

	// Filters describes what the generated runtime query builder may write into a predicate
	// it appends: the dialect's extra comparison operators, the functions that read a path
	// from a JSON column, and the conversions a filtered column may take.
	Filters FilterSyntax

	// Lists describes membership tests for caller-supplied lists, including empty IN and NOT
	// IN tests and optional array forms.
	Lists ListSyntax

	// Pagination describes how the dialect writes the row limit and offset the generated
	// runtime query builder appends. Every dialect must give the form of the limit.
	Pagination PaginationSyntax

	// Drivers lists supported Go driver module paths in preference order. Emitters use the
	// first recognised driver unless one is explicitly selected.
	Drivers []string

	// RowIDAliases lists the names of the implicit row identifier column every ordinary
	// table has without declaring it, matched as identifiers, or nothing when the dialect
	// has no such column. CoreTypes.RowID is its type.
	RowIDAliases []string

	// SystemColumns lists implicit table columns that queries can reference but star
	// expansion excludes. Row identifier aliases are listed separately.
	SystemColumns []string

	// SchemaSearchPath lists schemas used to resolve unqualified names, in order. New
	// unqualified objects use the first existing schema, or the first entry when none exists
	// in the catalogue.
	SchemaSearchPath []string

	// Lexical defines the dialect's SQL quoting, comments, placeholders and statement
	// boundaries. The zero value uses standard SQL rules.
	Lexical sqllex.Rules

	// Expressions is the set of expression kinds the dialect supports; the analyser reports
	// any other kind as unsupported.
	Expressions ast.ExpressionFeature

	// MaxParameters is the most parameters one statement may bind, which the generated code
	// respects when it expands a slice or batches rows. Zero means the engine sets no limit,
	// and the generated code keeps to a conservative default.
	MaxParameters int

	// SelectAliases lists clauses that can refer to select-list aliases. Including
	// ClauseSelectList also lets later select items reference earlier ones.
	SelectAliases ast.Clause

	// SelectAliasOperands lists clauses of SelectAliases that allow aliases inside
	// expressions, letting compared parameters take the aliased type. Visible columns take
	// precedence over same-name aliases; the empty default leaves these aliases unresolved
	// and their parameters untyped.
	SelectAliasOperands ast.Clause

	// Identifiers is how the dialect treats the letter case of identifiers. Its parser folds
	// the identifiers it reads accordingly, so the analyser compares names exactly, or
	// regardless of case when the dialect matches them that way.
	Identifiers sqlmodel.IdentifierCase

	// Returning lists statement kinds that support RETURNING. An empty set means none are
	// supported.
	Returning ast.DataChange

	// AsyncMutations reports whether the dialect has mutations that complete asynchronously
	// on the server, such as ClickHouse ALTER UPDATE, and so supports the asyncexec query
	// command.
	AsyncMutations bool

	// ScalarFunctionsInFrom permits scalar calls in FROM as one-row, one-column relations,
	// with the call's type and nullability and the function or alias as the column name. It
	// excludes aggregate and set-returning calls; false requires a table-valued function.
	ScalarFunctionsInFrom bool

	// UngroupedColumns is how the engine treats a column a query reads outside every
	// aggregate when the query aggregates without a GROUP BY clause, as in SELECT name,
	// count(*) FROM items, which gives one row however many rows it reads. The zero value,
	// UngroupedColumnsNullable, types such a column nullable.
	UngroupedColumns UngroupedColumnRule

	// ViewsFollowTheirTables refreshes view columns after changes to tables or views, so
	// star projections reflect new columns and current types. False fixes the columns when
	// the view is created.
	ViewsFollowTheirTables bool
}

const (
	// UngroupedColumnsNullable is an engine that returns the query's one row, reading such a
	// column from one of the rows the query read, or NULL when it read none, so the analyser
	// types the column nullable.
	UngroupedColumnsNullable UngroupedColumnRule = iota

	// UngroupedColumnsWarned is an engine whose mode decides: it rejects the query in its
	// default mode and otherwise returns the row as UngroupedColumnsNullable describes, so
	// the analyser warns and types the column nullable.
	UngroupedColumnsWarned

	// UngroupedColumnsRejected is an engine that rejects the query, so the analyser reports
	// an error.
	UngroupedColumnsRejected
)

// Clone returns a copy of the Info that shares no slice or map with it, so either can be
// changed without changing the other. The lexical rules' callbacks are shared, as a
// function cannot be copied.
//
// Returns Info which is the copy, with a nil slice or map left nil, or the zero Info for
// a nil receiver.
func (info *Info) Clone() Info {
	if info == nil {
		return Info{}
	}
	clone := *info
	clone.OperatorFeatures = maps.Clone(info.OperatorFeatures)
	clone.Placeholders.Sigils = slices.Clone(info.Placeholders.Sigils)
	clone.Filters.Operators = slices.Clone(info.Filters.Operators)
	clone.Filters.JSONPathFunctions = slices.Clone(info.Filters.JSONPathFunctions)
	clone.Filters.Casts = slices.Clone(info.Filters.Casts)
	clone.Filters.CastTypes = slices.Clone(info.Filters.CastTypes)
	clone.Drivers = slices.Clone(info.Drivers)
	clone.RowIDAliases = slices.Clone(info.RowIDAliases)
	clone.SystemColumns = slices.Clone(info.SystemColumns)
	clone.SchemaSearchPath = slices.Clone(info.SchemaSearchPath)
	clone.Lexical.Parameters.BracedLiteralWords = slices.Clone(info.Lexical.Parameters.BracedLiteralWords)
	return clone
}

// UngroupedColumnRule is how an engine treats a column a query reads outside every
// aggregate when the query aggregates without a GROUP BY clause, as Info.UngroupedColumns
// declares it.
type UngroupedColumnRule uint8

// CoreTypes names the dialect's own types for the values the analyser types itself rather
// than reading them from a column or a function signature. A dialect builds each with its
// own normaliser, so the analyser never invents an engine's type name.
type CoreTypes struct {
	// Boolean is the type of a comparison, an IS NULL, IN, BETWEEN or EXISTS test, and a
	// logical operator, unless the dialect types them itself as a PredicateTyperPort. An IS
	// NULL or EXISTS test is never NULL; any other is NULL when an operand it reads may be.
	Boolean sqlmodel.SQLType

	// Text is the dialect's general character string type.
	Text sqlmodel.SQLType

	// Count is the type of a row count, as COUNT(*) returns it.
	Count sqlmodel.SQLType

	// JSON is the dialect's JSON document type, or a type of the unknown category when the
	// dialect has none.
	JSON sqlmodel.SQLType

	// RowCount is the type of a parameter bound to LIMIT or OFFSET.
	RowCount sqlmodel.SQLType

	// RowID is the type of the implicit row identifier column Info.RowIDAliases names. It is
	// read only when the dialect lists aliases.
	RowID sqlmodel.SQLType

	// Void describes a function result that carries no value. Queries returning only this
	// type have no output to read.
	Void sqlmodel.SQLType
}

// RowRelation is a relation of a query that the query names where a value is expected, as
// RowValueTyperPort types it.
type RowRelation struct {
	// Name is the name of the table or view the relation reads, or empty for a relation the
	// catalogue does not hold, such as a derived table, a common table expression or a table
	// function.
	Name string

	// Schema is the schema of that table or view, or empty.
	Schema string

	// Columns holds the relation's columns in order, with the nullability the query gives
	// them.
	Columns []ast.ScopedColumn

	// Function reports a relation a table-valued function produces.
	Function bool

	// OuterJoined reports a relation on the nullable side of an outer join, which has no row
	// of its own where the join matched nothing.
	OuterJoined bool
}

// ResultProblem is why a dialect's drivers cannot read a value a query returns, as a
// ResultCheckerPort reports it.
type ResultProblem struct {
	// Reason says why the drivers cannot read the value, in the dialect's terms, such as
	// that the driver cannot read a STRUCT whose fields have no names.
	Reason string

	// Suggestion says how to write the query so the drivers can read the value, such as
	// naming the struct's fields.
	Suggestion string
}

// CallArgument is an argument of a function call as an ArgumentCoercerPort sees it.
type CallArgument struct {
	// Type is the argument's type, as the dialect types the expression the call passes.
	Type sqlmodel.SQLType

	// Literal marks an argument written directly as a SQL literal. Dialects may use this to
	// infer its type from a function parameter.
	Literal bool
}

// ArgumentCoercion is how an engine converts a function's argument to the type of its
// parameter, as an ArgumentCoercerPort reports it.
type ArgumentCoercion struct {
	// MayBeNull reports that the engine gives NULL for an argument it cannot convert, such
	// as a string that holds no date, rather than failing the statement, so a call with an
	// argument converted this way may be NULL whatever its arguments.
	MayBeNull bool
}
