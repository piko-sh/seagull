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

package gogen

import (
	"go/ast"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Driver is what a driver API supplies to code generation: as data, how the generated
// code calls its connection, how it writes slice lists and what its querier holds; and,
// through the optional interfaces, what data cannot say.
//
// A nil interface leaves the generated code with the behaviour every driver shares.
type Driver struct {
	// Binder binds the parameters the driver takes in an envelope, or is nil when it binds
	// every parameter as it is.
	Binder ParameterBinder

	// Reader says how the generated code reads the columns the driver cannot scan as they
	// are, or is nil when it scans every column as it is.
	Reader ColumnReader

	// Helpers adds the driver's helper files to a package, or is nil when it has none.
	Helpers HelperProvider

	// Batch generates the methods of batch and copyfrom queries, or is nil when the driver
	// runs them as plain statements.
	Batch BatchBuilder

	// Connection says how the generated code calls the connection.
	Connection Connection

	// Querier describes the querier file.
	Querier emitkit.QuerierSpec

	// Lists says how slice lists are written; only ListExpand expands them at run time.
	Lists emitkit.ListMode

	// Enums says where the driver handles the values of the enum types the package declares
	// only as Go's own string, so the generated code scans and binds them through strings
	// and converts them. Its zero value converts nothing.
	Enums EnumStrings
}

// Connection says how the generated code calls a driver's connection: the method names,
// the fields it calls them on, and what they return. The calls themselves, the receiver
// and the imports follow from it.
type Connection struct {
	// ExecResult is the type the Exec method returns besides an error, such as sql.Result,
	// or nil when Exec returns only an error.
	ExecResult *sqlmodel.GoType

	// Query is the method that runs a query returning rows, such as QueryContext.
	Query string

	// QueryRow is the method that runs a query returning one row, such as QueryRowContext.
	QueryRow string

	// Exec is the method that runs a statement returning no rows, such as ExecContext.
	Exec string

	// Reader is the Queries field read-only queries run on.
	Reader string

	// Writer is the Queries field every other query runs on.
	Writer string

	// CountScan is the Go type a runtime builder's Count scans the count into before
	// converting it to the int64 it returns, or empty for int64 itself.
	CountScan string

	// NoRows is the error value the driver returns when a query that must return one row
	// returns none.
	NoRows sqlmodel.GoType

	// RowsAffectedErrors reports that ExecResult's RowsAffected returns an error as well as
	// the count.
	RowsAffectedErrors bool
}

// Query supplies a driver builder with analysed SQL, planned names, type mappings and
// imports.
//
// Builders use this prepared information when emitting methods.
type Query struct {
	// Analysed is the query.
	Analysed *sqlmodel.AnalysedQuery

	// Naming holds the query's names from the package's naming plan: its method, SQL and
	// count constants, Params and Row structs, fields and locals. It is never nil.
	Naming *emit.QueryNaming

	// Types maps each SQL type to its Go type.
	Types sqlmodel.GoTypeResolver

	// Imports collects the imports of the file the query is written to.
	Imports *goast.ImportTracker

	// Renderer writes SQL using the driver's placeholders, list forms and column
	// conversions. It is always set.
	Renderer *emitkit.Renderer

	// Count is the query's CountTemplate written with Renderer, wrapping no column, with its
	// own numbering, arguments and filter mark: the text of the count constant a runtime
	// builder's Count terminal starts from. It is nil for any query that is not a runtime
	// builder or whose rows cannot be counted.
	Count *emitkit.Rendered

	// Rendered is the query's Template written with Renderer: the text of its SQL constant,
	// the arguments the driver binds in binding order, and the marks at which the generated
	// code edits the SQL at run time (slice lists, the filter, the ORDER BY and pagination
	// places, the VALUES row).
	Rendered emitkit.Rendered
}

// ParameterBinder is an optional interface a driver implements to bind some parameters
// through an envelope, such as a named value or a driver.Valuer, rather than as they are.
type ParameterBinder interface {
	// Bind returns the expression that binds a parameter.
	//
	// Takes access (ast.Expr) which reads the parameter's value.
	// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
	// Takes imports (*goast.ImportTracker) which collects the imports the expression needs.
	//
	// Returns ast.Expr which binds the parameter, or access when it binds as it is.
	Bind(access ast.Expr, parameter *sqlmodel.QueryParameter, imports *goast.ImportTracker) ast.Expr
}

// PositionalBinder lets a ParameterBinder adapt values for positional SQL whose
// parameters the driver formats into the statement text. Without it, positional
// parameters are bound unchanged.
type PositionalBinder interface {
	// BindPositional returns the expression that binds a parameter in the positional form.
	//
	// Takes access (ast.Expr) which reads the parameter's value.
	// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
	//
	// Returns ast.Expr which binds the parameter, or access when it binds as it is.
	BindPositional(access ast.Expr, parameter *sqlmodel.QueryParameter) ast.Expr
}

// ColumnReader is an optional interface a driver implements to say how the generated code
// reads an output column of a SQL type its driver cannot scan as it is. A driver without
// it reads every column as the driver returns it.
type ColumnReader interface {
	// ColumnReading returns how the generated code reads an output column of a SQL type.
	//
	// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
	//
	// Returns ColumnReading which says how the column is read.
	ColumnReading(sqlType sqlmodel.SQLType) ColumnReading
}

// ColumnConverter is an optional interface a ColumnReader implements when it reads some
// columns as ColumnReadConverted or ColumnReadAdapted: it names the scan adapter that
// converts the value the driver returns for such a column into the column's Go type.
//
// A reader without it has every column it reads so read as it is.
type ColumnConverter interface {
	// ConvertingScan returns the scan argument that reads a column into a destination
	// through the driver's converting scan adapter, such as
	// sglScanDriverComposite(&row.Tags).
	//
	// Takes destination (ast.Expr) which is the address the column is read into.
	//
	// Returns ast.Expr which is the scan argument.
	ConvertingScan(destination ast.Expr) ast.Expr
}

// OverrideReader selects scan adapters for overridden column Go types.
//
// Adapters convert driver values to the requested type or use its sql.Scanner
// implementation. Other readers scan overridden columns directly.
type OverrideReader interface {
	// ReadsOverride reports whether the generated code reads a column of a SQL type whose Go
	// type an override chose through the adapter.
	//
	// Takes goType (sqlmodel.GoType) which is the column's Go type, as the override chose
	// it, a pointer for a nullable column.
	// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
	//
	// Returns bool which is true when the column is read through the adapter.
	ReadsOverride(goType sqlmodel.GoType, sqlType sqlmodel.SQLType) bool

	// OverrideScan returns the scan argument that reads a column into a destination through
	// the adapter, such as sglScanOverride(&row.Label).
	//
	// Takes destination (ast.Expr) which is the address the column is read into.
	// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type, which may choose the
	// adapter, such as one that decodes a JSON document.
	//
	// Returns ast.Expr which is the scan argument.
	OverrideScan(destination ast.Expr, sqlType sqlmodel.SQLType) ast.Expr
}

// RawConverter describes the driver value and conversion used after scanning a
// ColumnReadRaw column. Readers without it scan raw columns directly.
type RawConverter interface {
	// RawType returns the Go type the driver fills for a column of a SQL type, such as
	// map[string]any for a named tuple.
	//
	// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
	//
	// Returns sqlmodel.GoType which is the raw local's Go type.
	RawType(sqlType sqlmodel.SQLType) sqlmodel.GoType

	// ConvertRaw returns the call that converts a raw local into a destination after the
	// scan, which returns an error when it cannot, such as sglReadNativeColumn(&row.Fields,
	// raw.Fields).
	//
	// Takes destination (ast.Expr) which is the address the column is read into.
	// Takes raw (ast.Expr) which reads the raw local.
	//
	// Returns ast.Expr which is the call.
	ConvertRaw(destination ast.Expr, raw ast.Expr) ast.Expr
}

// HelperProvider is an optional interface a driver implements to add helper files to a
// package, such as the functions its parameter envelopes call.
type HelperProvider interface {
	// Helpers returns the helper files a request's package needs.
	//
	// Takes request (*emit.Request) which is the request being emitted. Helpers does not
	// change it.
	//
	// Returns []sqlmodel.GeneratedFile which holds the helper files, or none.
	// Returns error when a helper cannot be generated.
	Helpers(request *emit.Request) ([]sqlmodel.GeneratedFile, error)
}

// BatchBuilder is an optional interface a driver implements to generate the methods of
// batch and copyfrom queries. A driver without it runs both as plain statements.
type BatchBuilder interface {
	// Batch returns the method of a batch query.
	//
	// Takes query (*Query) which is the query being generated.
	//
	// Returns ast.Decl which is the method's declaration.
	// Returns error when the query cannot be batched.
	Batch(query *Query) (ast.Decl, error)

	// CopyFrom returns the method of a copyfrom query.
	//
	// Takes query (*Query) which is the query being generated.
	//
	// Returns ast.Decl which is the method's declaration.
	// Returns error when the query cannot be copied in.
	CopyFrom(query *Query) (ast.Decl, error)

	// RepeatsRow reports whether the batch method of a query writes its VALUES row once per
	// row, so the SQL the method runs is the query's SQL edited from the row on; a method
	// that queues or appends each row runs the query's SQL as it is.
	//
	// Takes query (*Query) which is the batch query being generated.
	//
	// Returns bool which is true when the method repeats the row.
	RepeatsRow(query *Query) bool

	// CopyStatement says what SQL text the copyfrom method of a query runs, so the
	// query-names lookup can name the method by it.
	//
	// Takes query (*Query) which is the copyfrom query being generated.
	//
	// Returns CopyStatement which says what the method runs.
	CopyStatement(query *Query) CopyStatement
}

// CopyStatement says what SQL text the copyfrom method of a query runs.
type CopyStatement uint8

const (
	// CopyStatementNone is a method that runs no SQL text, such as one that streams its rows
	// through a copy protocol.
	CopyStatementNone CopyStatement = iota

	// CopyStatementAsIs is a method that runs the query's SQL as it is, such as one that
	// appends each row to a batch it prepares from the SQL.
	CopyStatementAsIs

	// CopyStatementRepeatsRow is a method that runs the query's SQL edited from its VALUES
	// row on, which it writes once per row.
	CopyStatementRepeatsRow
)
