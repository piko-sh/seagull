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
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// InlineJSONTypeName is the name of the JSON type an emitter declares in the generated
	// package when the JSON type slot keeps its default. Request.Types resolves such a JSON
	// value to a GoType with an empty Package and this Name, and an emitter whose output
	// refers to it declares it.
	InlineJSONTypeName = emit.InlineJSONTypeName

	// QueryCommandOne reads the first row. A zero-row result is an error unless the query
	// sets Optional, which adds a boolean result indicating whether a row was found.
	QueryCommandOne = sqlmodel.QueryCommandOne

	// QueryCommandMany returns a slice of rows.
	QueryCommandMany = sqlmodel.QueryCommandMany

	// QueryCommandExec executes without returning rows.
	QueryCommandExec = sqlmodel.QueryCommandExec

	// QueryCommandExecResult returns the driver's execution result.
	QueryCommandExecResult = sqlmodel.QueryCommandExecResult

	// QueryCommandExecRows returns the affected row count.
	QueryCommandExecRows = sqlmodel.QueryCommandExecRows

	// QueryCommandBatch executes using the driver's batch strategy.
	QueryCommandBatch = sqlmodel.QueryCommandBatch

	// QueryCommandStream returns an iterator over rows.
	QueryCommandStream = sqlmodel.QueryCommandStream

	// QueryCommandCopyFrom uses the driver's bulk-insert strategy, such as PostgreSQL COPY.
	QueryCommandCopyFrom = sqlmodel.QueryCommandCopyFrom

	// QueryCommandAsyncExec marks a fire-and-forget statement whose completion semantics are
	// server-side asynchronous.
	//
	// ClickHouse ALTER UPDATE and ALTER DELETE are the canonical case: the server accepts
	// the statement, queues a background mutation, and the client receives no rows-affected
	// count. The emitted method returns (error) only and carries a doc comment explaining
	// the asynchronous lifecycle so callers do not expect synchronous completion. A dialect
	// without asynchronous mutations rejects this command at analysis time.
	QueryCommandAsyncExec = sqlmodel.QueryCommandAsyncExec

	// SegmentText is SQL text, written as Text holds it.
	SegmentText = sqlmodel.SegmentText

	// SegmentParameter is one placeholder occurrence of the parameter at index Parameter of
	// AnalysedQuery.Parameters.
	//
	// TypeText is the type a typed placeholder form carries, as the query writes it, such as
	// UInt64 in {id:UInt64}, and is empty for a form without a type. A renderer writes the
	// placeholder in the form the code executes.
	SegmentParameter = sqlmodel.SegmentParameter

	// SegmentList represents a slice parameter used as an IN or NOT IN list. The renderer
	// chooses expanded placeholders or the dialect's array form.
	SegmentList = sqlmodel.SegmentList

	// SegmentColumn represents a top-level output item and its expression.
	//
	// Renderers can wrap the expression while preserving its output alias.
	SegmentColumn = sqlmodel.SegmentColumn

	// SegmentStar represents a star output item and the columns used to expand it. AsWritten
	// preserves the original star unless one of its columns needs wrapping.
	SegmentStar = sqlmodel.SegmentStar

	// SegmentFilter is the WHERE condition a dynamic query or a runtime builder adds
	// predicates to.
	//
	// When Present, Children are the condition alone, the WHERE keyword preceding the
	// segment as text. Otherwise the segment has no children and marks where a WHERE clause
	// goes; code inserting one writes its own leading space.
	SegmentFilter = sqlmodel.SegmentFilter

	// SegmentOrderBy is the ORDER BY clause, its keywords and modifiers included, as
	// Children when Present; otherwise it has no children and marks where the clause goes.
	SegmentOrderBy = sqlmodel.SegmentOrderBy

	// SegmentPagination is the LIMIT, OFFSET and FETCH clauses as written, keywords
	// included, as Children when Present; otherwise it has no children and marks where they
	// go.
	SegmentPagination = sqlmodel.SegmentPagination

	// SegmentValuesRow is the single row of an INSERT ... VALUES with exactly one row, from
	// its opening parenthesis through its closing one, as Children.
	SegmentValuesRow = sqlmodel.SegmentValuesRow

	// ParameterDirectiveNone marks parameters inferred without a sgl.param declaration and
	// directive specs whose roles declare no parameter.
	ParameterDirectiveNone = sqlmodel.ParameterDirectiveNone

	// ParameterDirectiveParam defines a standard query parameter.
	ParameterDirectiveParam = sqlmodel.ParameterDirectiveParam

	// ParameterDirectiveSortable defines a sortable input (dynamic ORDER BY) declared with
	// the standalone sgl.sortable directive; it does not bind a placeholder.
	ParameterDirectiveSortable = sqlmodel.ParameterDirectiveSortable

	// SeverityError marks a Problem that stops code generation for the package.
	SeverityError = diag.SeverityError

	// SeverityWarning marks a Problem that is reported but does not block generation.
	SeverityWarning = diag.SeverityWarning

	// SeverityHint marks a Problem that suggests an improvement.
	SeverityHint = diag.SeverityHint

	// CodeEmitterUnsupported is the diagnostic code, Q059, for a query or an option the
	// configured emitter cannot generate code for, such as a command its driver has no way
	// to run. A CheckerPort reports such a Problem under it.
	CodeEmitterUnsupported = diag.CodeEmitterUnsupported
)

// The ports an emitter implements.
type (
	// EmitterPort is the port to implement for a new driver API: it turns analysed queries
	// into Go source written against that API.
	//
	// It has a single method, Emit, so the request can grow without breaking
	// implementations: new inputs are added as fields of Request. Emit must not change the
	// request, and its files must depend only on the request, not on the order of its
	// queries. Each file has a plain name, unique in the package, starts with the style's
	// header line and is formatted as gofmt would.
	//
	// emit_kit holds the building blocks the built-in emitters share, and emit_testkit.Run
	// checks an implementation against the conformance suite. The built-in implementations
	// are emit_driver_databasesql, emit_driver_pgx and emit_driver_clickhouse.
	EmitterPort = emit.EmitterPort

	// TypeDefaulterPort is an optional port an EmitterPort implements to supply type slots
	// that suit its driver better than the built-in defaults.
	//
	// Its slots rank above the built-in defaults and below the configured preset slots, type
	// overrides and go_type directives. An emitter serving several drivers chooses the
	// driver from the Drivers of the dialect's facts, never from the dialect's name or
	// family.
	TypeDefaulterPort = emit.TypeDefaulterPort

	// NullElementBinderPort is an optional port an EmitterPort implements to say whether its
	// driver binds a NULL element of an array or list parameter, and a NULL value of a map
	// parameter. An emitter without it is assumed to bind them.
	//
	// Where the driver cannot, parameters use plain element values by default, and the
	// emitter's CheckerPort should refuse a parameter or source column that explicitly
	// declares nullable elements.
	NullElementBinderPort = emit.NullElementBinderPort

	// CheckerPort is an optional port an EmitterPort implements to refuse, before emission,
	// the queries or settings its driver cannot serve, such as a command the driver has no
	// way to run.
	//
	// The generator calls Check once per package, after planning the package's names, so
	// Request.Naming is set, and before Emit. It reports each problem as a diagnostic at the
	// query's file and line, or against the package when it names no query. When any problem
	// is an error it does not call Emit, so the package gets no files. Check is not called
	// for a caller that calls Emit directly, so an emitter refuses the same requests from
	// Emit with an error.
	CheckerPort = emit.CheckerPort

	// ReserverPort is an optional port an EmitterPort implements to say which names its
	// generated code uses besides those the naming plan chooses, so the plan never chooses
	// one of them.
	//
	// Reserve depends only on the dialect and the style, since the generator also calls it
	// when it is built, to check the configured query-names file and function. An emitter
	// without it gets emit_kit.DefaultReservation, which covers the names of every built-in
	// emitter.
	ReserverPort = emit.ReserverPort
)

// What an emitter is given and what it returns.
type (
	// Request carries everything one emission needs: the package name, the catalogue, the
	// analysed queries, the type mapping, the dialect's facts, the style of the generated
	// code and the naming plan. An emitter must not change it.
	Request = emit.Request

	// Style controls the vocabulary and idioms of generated code.
	//
	// It sets the file header, the prefix of generated helper identifiers, the prefix of
	// generated error text, the optional query-names file and interface, and struct tags.
	// The types of its fields are named in the root package, such as seagull.Interface and
	// seagull.JSONTags.
	Style = emit.Style

	// GeneratedFile is one generated source file: its name and its formatted content.
	// EmitterPort.Emit returns one for each file of the package.
	GeneratedFile = sqlmodel.GeneratedFile

	// GoTypeResolver maps a SQL type to the Go type generated code uses for it.
	// Request.Types holds the one the generator builds from the configured type overrides,
	// type slots and defaults.
	GoTypeResolver = sqlmodel.GoTypeResolver

	// GoType identifies a Go type by its import path and name, or describes a map, struct or
	// generic instance built from other Go types. It is the same type as seagull.GoType.
	GoType = sqlmodel.GoType

	// TypeSlot names the Go types one category of SQL values maps to.
	//
	// The zero value keeps the default for the slot. The root package exports it as
	// seagull.Slot.
	TypeSlot = sqlmodel.TypeSlot

	// TypeSlots names the Go types for the SQL values that have no single natural Go type,
	// or whose natural type a driver cannot read: exact decimals, money, integers wider than
	// 64 bits, JSON, intervals, IP addresses, enums, maps and structs.
	//
	// A zero slot keeps the default. A TypeDefaulterPort returns the slots its driver reads
	// natively.
	TypeSlots = sqlmodel.TypeSlots
)

// The naming plan, which fixes every name the generated code declares.
type (
	// Naming is a package's naming plan: every name the generated code declares, and the
	// qualifier of every package it imports, chosen once for the whole package so that no
	// two collide and every emitter writes the same names.
	//
	// The generator plans it before emission, after asking the emitter what it reserves (see
	// ReserverPort), and passes it in Request.Naming. An emitter takes each name from the
	// plan and never derives one itself; a name a slice holds no entry for is one the query
	// does not need. The zero Naming means no plan was made.
	//
	// The plan guarantees that the package-level names it chooses (SQL constants, types,
	// variables, model and named types, enum constants) are unique among themselves and
	// differ from every import qualifier and every declaration, method and file the emitter
	// reserves; that a method argument or local it chooses differs from the emitter's locals
	// and receiver, every import qualifier, every package-level name a method body may refer
	// to and every other argument or local of the same method; and that the fields it
	// chooses are unique within their struct. Every name is a valid Go identifier, and every
	// exported name it chooses is exported. The plan is deterministic: the same request and
	// reservation give the same plan whatever the order of the queries.
	Naming = emit.Naming

	// QueryNaming holds the names one query's generated code declares and uses.
	//
	// A name the query does not need is empty, and a slice it does not need is nil. A slice
	// the query needs is exactly as long as the slice of the query it is parallel to.
	QueryNaming = emit.QueryNaming

	// ModelNaming names the model of one catalogue table.
	ModelNaming = emit.ModelNaming

	// NamedType is a Go type the package declares for an enum or a structured SQL type, so
	// every model and row holding a value of that type uses the same one.
	NamedType = emit.NamedType

	// Reservation lists the names an emitter's generated code uses whatever the queries are.
	//
	// It reserves every name the emitter may write, not only those one package needs, so the
	// plan for a package does not depend on which helper files it gets. A ReserverPort
	// returns one; emit_kit.DefaultReservation is the one an emitter without it gets.
	Reservation = emit.Reservation
)

// The analysed queries a request carries.
type (
	// AnalysedQuery is the fully resolved result of query analysis, with all types and
	// nullability propagated.
	AnalysedQuery = sqlmodel.AnalysedQuery

	// QueryParameter is a fully typed query parameter.
	QueryParameter = sqlmodel.QueryParameter

	// OutputColumn is a fully typed output column.
	OutputColumn = sqlmodel.OutputColumn

	// AllowedColumn is a column a runtime query builder may filter or sort on.
	AllowedColumn = sqlmodel.AllowedColumn

	// SQLTemplate is a query's executable SQL as segments: text as written, and the parts
	// code generation renders per driver, which are the placeholders, slice lists, output
	// columns and the places a dynamic query edits. Rendering it in a dialect's placeholder
	// form gives driver-correct SQL; see emit_kit.Render.
	//
	// The analyser builds it from the statement's spans. Statements before the query's own
	// stay as text, and directive comment lines are left out. The zero template holds no
	// SQL.
	SQLTemplate = sqlmodel.SQLTemplate

	// SQLSegment is one part of a SQLTemplate.
	//
	// Which fields it uses depends on its Kind; see the SQLSegmentKind constants. A field a
	// kind does not use is zero.
	SQLSegment = sqlmodel.SQLSegment
)

// QueryCommand specifies the execution pattern for a query.
type QueryCommand = sqlmodel.QueryCommand

// SQLSegmentKind says what one segment of a SQLTemplate holds.
type SQLSegmentKind = sqlmodel.SQLSegmentKind

// ParameterDirectiveKind identifies the role of a parameter or sortable input declared in
// a query header.
//
// Cardinality (slice) and presence (optional) are represented by IsSlice and IsOptional.
// LIMIT and OFFSET are inferred from clause position. The zero value marks the absence of
// a parameter directive.
type ParameterDirectiveKind = sqlmodel.ParameterDirectiveKind

// Problem is something an emitter or a code generation tool finds wrong with a query or a
// request, reported as a diagnostic rather than as an error from Emit.
//
// A problem names its query, or none for the request as a whole, and is reported under
// CodeEmitterUnsupported for what the driver cannot serve. A SeverityError problem stops
// emission.
type Problem = emit.Problem

// ErrorSeverity is how serious a Problem is: SeverityError, SeverityWarning or
// SeverityHint.
//
// It travels in JSON and other text encodings by name: error, warning or hint. It is the
// same type as seagull.ErrorSeverity.
type ErrorSeverity = diag.ErrorSeverity
