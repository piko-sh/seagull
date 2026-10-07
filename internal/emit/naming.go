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

package emit

import (
	"seagull.sh/seagull/internal/sqlmodel"
)

// Naming is a package's naming plan: every name the generated code declares, and the
// qualifier of every package it imports, chosen once for the whole package so that no two
// collide and every emitter writes the same names.
//
// The pipeline plans it before emission, after asking the emitter what it reserves (see
// ReserverPort), and passes it in Request.Naming. An emitter takes each name from the
// plan and never derives one itself; a name a slice holds no entry for is one the query
// does not need. The zero Naming means no plan was made.
//
// The plan guarantees that the package-level names it chooses (SQL constants, types,
// variables, model and named types, enum constants) are unique among themselves and
// differ from every import qualifier and every declaration, method and file the emitter
// reserves; that a method argument or local it chooses differs from the emitter's locals
// and receiver, every import qualifier, every package-level name a method body may refer
// to and every other argument or local of the same method; and that the fields it chooses
// are unique within their struct. Every name is a valid Go identifier, and every exported
// name it chooses is exported. The plan is deterministic: the same request and
// reservation give the same plan whatever the order of the queries.
type Naming struct {
	// Files maps query source files to their generated Go filenames. Names avoid collisions
	// and Go filename conventions that would hide or restrict the files.
	Files map[string]string

	// Imports maps import paths to the qualifiers shared by every generated file. Files use
	// explicit aliases when those qualifiers differ from the package names.
	Imports map[string]string

	// Interface is the interface of the query methods the package declares, as
	// Style.Interface names it, or empty when the style asks for none.
	Interface string

	// Models names the model of each catalogue table the package declares one for, in
	// declaration order: by table name, then by schema.
	Models []ModelNaming

	// Types lists the named Go types the package declares for enum and structured SQL types,
	// in declaration order. Request.Types resolves a SQL type a named type covers to it.
	Types []NamedType

	// Queries holds the names of each query, parallel to Request.Queries.
	Queries []QueryNaming
}

// QueryNaming holds the names one query's generated code declares and uses.
//
// A name the query does not need is empty, and a slice it does not need is nil. A slice
// the query needs is exactly as long as the slice of the query it is parallel to.
type QueryNaming struct {
	// Method is the Queries method that runs the query, such as GetUser: a snake_case query
	// name becomes PascalCase with common initialisms; other names have their first letter
	// upper-cased. The name is made unique among the Queries methods.
	Method string

	// SQLConstant is the package-level constant holding the query's SQL: the query's name in
	// lower camel case (as Style.SQLConstantLowerCase says) followed by
	// Style.SQLConstantSuffix, such as getUserSQL, made unique in the package.
	SQLConstant string

	// CountConstant is the package-level constant holding the SQL that counts the rows a
	// runtime builder query returns, for a query whose rows can be counted.
	CountConstant string

	// Label is the name the query-names lookup reports for the query's SQL, such as GetUser.
	// It labels telemetry, so it is not made unique.
	Label string

	// Params is the struct type holding the query's parameters, such as GetUserParams, or
	// empty when the generated method accepts its parameters as arguments of their own. A
	// batch or copyfrom method takes a slice of it.
	Params string

	// ParamsArgument is the method argument holding the Params struct, or the slice of them
	// for a batch or copyfrom method, such as params.
	ParamsArgument string

	// Row is the struct type a result row is read into, such as GetUserRow, for a query
	// returning rows.
	Row string

	// Builder is the runtime builder type of a query generated as one.
	Builder string

	// AllowedColumns is the package-level variable mapping each column a runtime builder
	// accepts to the SQL it writes for it.
	AllowedColumns string

	// OrderBy is the string type naming the columns a sortable query orders by.
	OrderBy string

	// DirectionField is the Params field holding the direction a sortable query orders in,
	// such as SortDirection.
	DirectionField string

	// ParameterFields holds, parallel to AnalysedQuery.Parameters, the Params field each
	// parameter is read from, or the name of the method argument holding it when there is no
	// Params struct.
	ParameterFields []string

	// ColumnFields holds, parallel to AnalysedQuery.OutputColumns, the field each column is
	// read into: a field of Row, or for an embedded column a field of its embed's struct.
	ColumnFields []string

	// ColumnLocals holds, parallel to AnalysedQuery.OutputColumns, the local variable each
	// column is scanned into before the rows are grouped, for a grouped query.
	ColumnLocals []string

	// Embeds names the struct of each embed group the query's rows hold, in the order the
	// groups first appear among the output columns.
	Embeds []EmbedNaming

	// OrderByConstants holds, parallel to the SortableColumns of the query's sortable
	// parameter, the OrderBy constant naming each column.
	OrderByConstants []string

	// SharedRow reports that Row names a table model or shared row type declared elsewhere.
	// ColumnFields and the query's column types describe the fields used by this query.
	SharedRow bool
}

// EmbedNaming names the struct one embed group is read into.
type EmbedNaming struct {
	// Group is the embed group, as OutputColumn.EmbedName holds it, or its EmbedTable when
	// the name is empty.
	Group string

	// Field is the field of the Row struct holding the group, unique among Row's fields.
	Field string

	// Type is the struct type of the group.
	Type string
}

// ModelNaming names the model of one catalogue table.
type ModelNaming struct {
	// Schema is the schema holding the table.
	Schema string

	// Relation is the table's name.
	Relation string

	// Type is the model's struct type.
	Type string

	// Fields holds, parallel to the table's columns, the field each column is read into, or
	// empty for a hidden column the model leaves out.
	Fields []string
}

// NamedType is a Go type the package declares for an enum or a structured SQL type, so
// every model and row holding a value of that type uses the same one.
type NamedType struct {
	// Name is the Go type's name.
	Name string

	// Constants holds, parallel to SQLType.EnumValues, the constant naming each value of an
	// enum, and is nil for any other type.
	Constants []string

	// Fields holds, parallel to SQLType.StructFields, the field holding each field of a
	// structured type, and is nil for any other type.
	Fields []string

	// SQLType is the SQL type the Go type declares.
	SQLType sqlmodel.SQLType
}

// Reservation lists the names an emitter's generated code uses whatever the queries are.
// It reserves every name the emitter may write, not only those one package needs, so the
// plan for a package does not depend on which helper files it gets.
type Reservation struct {
	// Receiver is the receiver of the Queries methods, such as queries.
	Receiver string

	// Locals reserves body-local names that planned arguments, column locals and SQL
	// constants must avoid.
	Locals []string

	// Imports lists the import paths the generated files may use, in the order their
	// qualifiers are claimed: of two paths implying the same package name, the first keeps
	// it.
	Imports []string

	// Declarations lists the package-level identifiers the emitter declares, such as
	// Queries, New, DBTX and every helper.
	Declarations []string

	// Methods lists the methods and fields the emitter declares on Queries besides one
	// method per query, such as WithTx and reader.
	Methods []string

	// Files lists the names of the files the emitter writes besides those holding queries,
	// such as querier.go.
	Files []string
}
