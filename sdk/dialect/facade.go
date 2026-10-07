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
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// PlaceholderNumbered is a placeholder followed by the parameter's number, counting from
	// one, such as $1. A parameter written twice is bound once.
	PlaceholderNumbered = dialect.PlaceholderNumbered

	// PlaceholderAnonymous is a placeholder that stands alone, such as ?.
	//
	// Each one binds the next argument in order, so a parameter written twice is bound
	// twice. A directive line still anchors one by number, counting the placeholders in the
	// order they appear.
	PlaceholderAnonymous = dialect.PlaceholderAnonymous

	// PlaceholderNamed is a placeholder followed by the parameter's name, such as :name.
	PlaceholderNamed = dialect.PlaceholderNamed

	// DataChangeInsert is an INSERT, or another statement that adds rows, such as an upsert.
	DataChangeInsert = ast.DataChangeInsert

	// DataChangeUpdate is an UPDATE.
	DataChangeUpdate = ast.DataChangeUpdate

	// DataChangeDelete is a DELETE.
	DataChangeDelete = ast.DataChangeDelete

	// DataChangeMerge is a MERGE, which may add, change and remove rows in one statement.
	DataChangeMerge = ast.DataChangeMerge

	// TypeCategoryInteger covers whole numbers of a fixed width, signed or unsigned; Width
	// and Unsigned describe them.
	TypeCategoryInteger = sqlmodel.TypeCategoryInteger

	// TypeCategoryFloat covers binary floating-point numbers; Width describes them.
	TypeCategoryFloat = sqlmodel.TypeCategoryFloat

	// TypeCategoryDecimal covers exact decimal numbers, with an optional precision and
	// scale; Monetary marks an amount of money.
	TypeCategoryDecimal = sqlmodel.TypeCategoryDecimal

	// TypeCategoryBoolean covers truth values.
	TypeCategoryBoolean = sqlmodel.TypeCategoryBoolean

	// TypeCategoryText covers character strings, fixed or varying in length.
	TypeCategoryText = sqlmodel.TypeCategoryText

	// TypeCategoryBinary covers byte strings, fixed or varying in length.
	TypeCategoryBinary = sqlmodel.TypeCategoryBinary

	// TypeCategoryTemporal covers dates, times of day, timestamps and intervals; Temporal
	// and TimeZone describe them.
	TypeCategoryTemporal = sqlmodel.TypeCategoryTemporal

	// TypeCategoryJSON covers JSON documents, stored as text or in a binary form.
	TypeCategoryJSON = sqlmodel.TypeCategoryJSON

	// TypeCategoryUUID covers universally unique identifiers.
	TypeCategoryUUID = sqlmodel.TypeCategoryUUID

	// TypeCategoryNetwork covers IP addresses, networks and hardware addresses; Network
	// describes them, and Width the size of an address type that holds one IP version.
	TypeCategoryNetwork = sqlmodel.TypeCategoryNetwork

	// TypeCategoryGeometric covers point, line, polygon, and similar types.
	TypeCategoryGeometric = sqlmodel.TypeCategoryGeometric

	// TypeCategoryEnum covers user-defined enum types.
	TypeCategoryEnum = sqlmodel.TypeCategoryEnum

	// TypeCategoryComposite covers user-defined composite types.
	TypeCategoryComposite = sqlmodel.TypeCategoryComposite

	// TypeCategoryArray covers array types wrapping another element type.
	TypeCategoryArray = sqlmodel.TypeCategoryArray

	// TypeCategoryRange covers ranges of another type's values.
	TypeCategoryRange = sqlmodel.TypeCategoryRange

	// TypeCategoryStruct covers records of typed fields, named or positional.
	TypeCategoryStruct = sqlmodel.TypeCategoryStruct

	// TypeCategoryMap covers maps from typed keys to typed values.
	TypeCategoryMap = sqlmodel.TypeCategoryMap

	// TypeCategoryUnion covers tagged unions, a value of one of several typed members.
	TypeCategoryUnion = sqlmodel.TypeCategoryUnion

	// TypeCategoryUnknown is the fallback for unrecognised types.
	TypeCategoryUnknown = sqlmodel.TypeCategoryUnknown

	// TypeCategoryAggregateState covers the intermediate state of an aggregate function,
	// stored in place of a finalised value; reading the value needs the engine's finalising
	// function. It is placed after TypeCategoryUnknown so earlier values stay stable in
	// serialised golden files.
	TypeCategoryAggregateState = sqlmodel.TypeCategoryAggregateState

	// MaxTypeDepth is how deeply Seagull follows the element, key, field and member types
	// nested in a SQL type.
	//
	// The root is at depth 0, and each nested type adds one level. Traversal stops at
	// MaxTypeDepth to keep deeply nested types from exhausting the stack.
	MaxTypeDepth = sqlmodel.MaxTypeDepth
)

// The dialect a caller chooses, and the facts generated code reads about it.
type (
	// Dialect is a SQL dialect as the generator and the tools around it hold one: the facts
	// generated code reads about it, and how it names its types. Each dialect_engine_*
	// provider's New returns one, and dialect_registry.New builds one by name.
	//
	// The zero Dialect holds no dialect: the generator refuses it, its Info is the zero
	// Facts, and every type it names is of the unknown category.
	Dialect = dialect.Dialect

	// Facts describes a dialect for generated code and the tools that run the generator.
	//
	// It records the dialect's name and family, how it writes parameters, identifiers,
	// lists, row limits and the predicates a runtime filter appends, the drivers it runs on,
	// the lexical rules its scripts split under and the statements that return rows.
	//
	// A Dialect's Info returns one, and an emitter reads one from its request. Facts belong
	// to the caller, which may keep or change them.
	Facts = dialect.Facts
)

// How a dialect writes the parameters of its queries.
type (
	// Placeholders describes how a dialect writes the parameters of its queries: the forms a
	// query or a directive line may write one in, and the form the SQL the generated code
	// executes writes each one in.
	//
	// Code generation renders every placeholder it writes from this description, so no
	// engine's parameter syntax is written anywhere else.
	Placeholders = dialect.Placeholders

	// PlaceholderSigil is one way of writing a query parameter: the text that opens it, what
	// follows, and the text that closes it, if any.
	PlaceholderSigil = dialect.PlaceholderSigil
)

// PlaceholderKind says what follows a placeholder's prefix.
type PlaceholderKind = dialect.PlaceholderKind

// DataChange is a set of the statements that change a table's rows, such as the
// statements a dialect accepts a RETURNING clause on, as Facts.Returning holds them. The
// zero value is the empty set.
type DataChange = ast.DataChange

// The schema catalogue, and the SQL types its columns and a query's values hold.
type (
	// Catalogue is the schema state of a database: its schemas, keyed by the dialect's
	// identifier rules through its Key method. The generator builds it by replaying the
	// migration files, or reads it from a schema source, which builds one with Schema,
	// Table, Column and SQLType values.
	Catalogue = sqlmodel.Catalogue

	// Schema is one schema of a Catalogue: its tables, views, enums, composite types,
	// domains, functions and sequences, each keyed by name.
	Schema = sqlmodel.Schema

	// Table is a table of a Schema: its columns in declaration order, its primary key,
	// indexes and constraints, and the migration that introduced it.
	Table = sqlmodel.Table

	// Column is a column of a table or a view: its name, SQL type and nullability, with the
	// overrides migration directives declare for it and the migration that introduced it.
	// NewColumn builds one.
	Column = sqlmodel.Column

	// TableName names a table as a query writes it, such as the table an INSERT adds rows
	// to.
	TableName = sqlmodel.TableName

	// MigrationOrigin records which migration file introduced or last changed a catalogue
	// object, so a diagnostic can point back to the DDL that made it.
	MigrationOrigin = sqlmodel.MigrationOrigin

	// SQLType is a SQL type as the generator reads it: its SQLTypeCategory, the name the
	// engine spells it with, and the parameters that refine it, such as an integer's width
	// or an array's element type. NewSQLType builds one.
	SQLType = sqlmodel.SQLType
)

// NewColumn returns a column of the given name, type, and nullability, with every other
// attribute unset.
//
// Takes name (string) which is the column's name.
// Takes sqlType (SQLType) which is the column's type.
// Takes nullable (bool) which reports whether the column permits NULL values.
//
// Returns Column which carries only the name, type, and nullability.
func NewColumn(name string, sqlType SQLType, nullable bool) Column {
	return sqlmodel.NewColumn(name, sqlType, nullable)
}

// NewSQLType returns a SQLType of the given category, named as the engine spells it, with
// every other attribute unset.
//
// Takes category (SQLTypeCategory) which classifies the type.
// Takes engineName (string) which is the engine's name for the type, or "" when it has
// none.
//
// Returns SQLType which carries only the category and the engine name.
func NewSQLType(category SQLTypeCategory, engineName string) SQLType {
	return sqlmodel.NewSQLType(category, engineName)
}

// SQLTypeCategory classifies SQL types into the broad categories the generator maps to Go
// types.
type SQLTypeCategory = sqlmodel.SQLTypeCategory
