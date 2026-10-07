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
	"context"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// InlineJSONTypeName names the JSON type an emitter declares in the generated package
	// when the JSON slot keeps its default: Request.Types resolves such a JSON value to a
	// GoType with an empty Package and this Name, and an emitter whose output refers to it
	// declares it.
	InlineJSONTypeName = "JSON"
)

// EmitterPort turns analysed queries into Go source for one driver API.
//
// The interface has a single method so the request can grow without breaking
// implementations: new inputs are added as fields of Request.
type EmitterPort interface {
	// Emit generates every Go source file for one package. It must not change the request,
	// which may be reused, and its output must depend only on the request, not on the order
	// of Request.Queries, so the same input always gives the same files.
	//
	// Takes request (*Request) which carries the package name, schema, analysed queries,
	// type mappings, dialect details, the style of the generated code and the naming plan.
	//
	// Returns []sqlmodel.GeneratedFile which holds the generated files: each a plain file
	// name, unique in the package, that starts with the style's header line, and Go source
	// formatted as gofmt would.
	// Returns error when emission cannot proceed, which stops the run. A query the driver
	// cannot serve is better refused through CheckerPort, which reports it as a diagnostic
	// at the query; an emitter that implements CheckerPort refuses the same requests here
	// too, for a caller that emits without checking first.
	Emit(ctx context.Context, request *Request) ([]sqlmodel.GeneratedFile, error)
}

// TypeDefaulterPort is an optional interface an EmitterPort implements to supply type
// slots that suit its driver better than the built-in defaults.
//
// Its slots rank above the built-in defaults and below the configured preset slots, type
// overrides and go_type directives.
type TypeDefaulterPort interface {
	// DefaultTypes returns the emitter's default type slots for a dialect; a zero slot keeps
	// the built-in default.
	//
	// Takes info (*dialect.Facts) which describes the dialect the queries are analysed with,
	// so an emitter serving several drivers can overlay only where one needs it. Choose the
	// driver from info.Drivers rather than from the dialect's name or family, so a flavour,
	// a custom name or a new engine on a known driver resolves alike. DefaultTypes does not
	// change it.
	//
	// Returns sqlmodel.TypeSlots which holds the emitter's slot defaults.
	DefaultTypes(info *dialect.Facts) sqlmodel.TypeSlots
}

// NullElementBinderPort is an optional EmitterPort interface describing support for NULL
// array or list elements and NULL map values. Emitters without it are assumed to support
// them.
//
// Drivers without this support use plain element values by default; CheckerPort should
// refuse parameters or source columns that explicitly declare nullable elements.
type NullElementBinderPort interface {
	// BindsNullElements reports whether the driver binds a NULL element of an array or a
	// list parameter, and a NULL value of a map parameter.
	//
	// Takes info (*dialect.Facts) which describes the dialect the queries are analysed with;
	// choose the driver from info.Drivers. BindsNullElements does not change it.
	//
	// Returns bool which is true when the driver binds them.
	BindsNullElements(info *dialect.Facts) bool
}

// CheckerPort is an optional interface an EmitterPort implements to refuse, before
// emission, the queries or settings its driver cannot serve, such as a command the driver
// has no way to run.
//
// The pipeline calls Check once per package, after planning the package's names, so
// Request.Naming is set, and before Emit. It reports each problem as a diagnostic at the
// query's file and line, or against the package when it names no query. When any problem
// is an error it does not call Emit, so the package gets no files. Check is not called
// for a caller that calls Emit directly, so an emitter refuses the same requests from
// Emit with an error, which for the built-in emitters wraps ErrRefused.
type CheckerPort interface {
	// Check returns the problems the emitter finds with a request.
	//
	// Takes request (*Request) which is the request about to be emitted. Check does not
	// change it.
	//
	// Returns []Problem which holds the problems, or nil when there are none.
	Check(request *Request) []Problem
}

// ReserverPort is an optional interface an EmitterPort implements to say which names its
// generated code uses besides those the naming plan chooses, so the plan never chooses
// one of them. An emitter without it gets a default reservation that covers the names of
// every built-in emitter.
type ReserverPort interface {
	// Reserve returns the names the emitter's generated code uses for a dialect and a style.
	// It must not depend on anything else: the pipeline also calls it when it is built, to
	// check the configured query-names file and function against it.
	//
	// Takes info (*dialect.Facts) which describes the dialect the queries are analysed with.
	// Reserve does not change it.
	// Takes style (*Style) which holds the helper and error prefixes the emitter's helper
	// names start with. Reserve does not change it.
	//
	// Returns Reservation which holds the names.
	Reserve(info *dialect.Facts, style *Style) Reservation
}

// Request carries everything one emission needs.
type Request struct {
	// Catalogue holds the schema the models are generated from.
	Catalogue *sqlmodel.Catalogue

	// Types maps SQL types to Go types using configured overrides, slots and defaults. A
	// column or parameter's explicit GoTypeOverride takes precedence.
	Types sqlmodel.GoTypeResolver

	// Package is the Go package name of the generated files.
	Package string

	// Style controls the names, header and optional files of the generated code.
	Style Style

	// Queries holds the analysed queries, none nil, in the order they were read. The output
	// must not depend on that order: where order shows, sort by Filename and Name, as the
	// naming plan does.
	Queries []*sqlmodel.AnalysedQuery

	// DirectivePrefixes lists the directive prefixes the queries were written with,
	// canonical first, such as sgl, so a message can name a directive the way the user
	// writes it, such as sgl.embed. The SQL to embed never holds a directive line: each
	// query's Template leaves them out.
	DirectivePrefixes []string

	// Naming supplies the package's planned declaration, file and import names. It is zero
	// when the caller supplied no plan.
	Naming Naming

	// Dialect holds the facts of the SQL dialect the queries were analysed with, such as how
	// parameters are written and identifiers quoted. Emitters choose their driver's
	// behaviour from its Drivers, never from its name or family.
	Dialect dialect.Facts
}

// Style controls the vocabulary of generated code: the file header, the prefix of
// generated helper identifiers, the prefix of generated error text, and the optional
// query-names file.
type Style struct {
	// Header is the first line of every generated file, such as "// Code generated by sgl.
	// DO NOT EDIT.".
	Header string

	// HelperPrefix prefixes every generated helper identifier, so "sgl" yields sglSplice,
	// and the unexported generated sentinel errors are named err<HelperPrefix in Go
	// casing>..., such as errSglClickHouseValue.
	HelperPrefix string

	// ErrorPrefix prefixes the text of every generated error, so "sgl" yields "sgl: too many
	// bind variables", and names the exported generated error values Err<ErrorPrefix in Go
	// casing>..., such as ErrSglUnknownColumn.
	ErrorPrefix string

	// JSONScanInto names the generic function that adapts a typed destination into a
	// JSON-decoding sql.Scanner. The zero value selects the inline <HelperPrefix>ScanInto
	// helper emitted alongside the inline JSON type.
	JSONScanInto sqlmodel.GoType

	// PackageNames maps import paths to explicit package names when inference from the path
	// gives the wrong name. Generated imports use these names so they compile even when a
	// package's name differs from its directory.
	PackageNames map[string]string

	// SQLConstantSuffix is appended to query names when naming their SQL constants. Empty
	// adds no suffix.
	SQLConstantSuffix string

	// QueryNames controls the optional file that maps SQL text to query names.
	QueryNames QueryNames

	// StructTags maps keys to tags added to generated column fields alongside json, with
	// column tags taking precedence. In each value, {name} uses the safe, unique column name
	// regardless of JSONTags case styling; nil adds no tags.
	StructTags map[string]string

	// Interface controls the optional interface of the query methods.
	Interface Interface

	// JSONTags shapes the json tag every generated field holding a column carries. The zero
	// value writes the column's name as it is written.
	JSONTags JSONTags

	// SQLConstantLowerCase lower-cases each underscore-separated part of a query name before
	// joining the parts in SQL constant and related query variable names.
	SQLConstantLowerCase bool

	// KeepStars preserves SELECT *, RETURNING * and qualified stars such as t.*. By default
	// stars expand to explicit columns, keeping methods' reads stable when migrations add
	// columns before the code is regenerated.
	KeepStars bool

	// EmptySlices makes a many method, and a runtime builder's All, return an empty slice
	// rather than nil when no row matches, so encoding/json writes [] rather than null.
	EmptySlices bool

	// ReuseModels reads matching query rows into table models instead of separate Row
	// structs. A query must read only that table's columns with matching field names, Go
	// types and struct tags in any order; explicit model: or row: directives take
	// precedence.
	ReuseModels bool

	// DocComments writes doc comments from the SQL: a query's description, the prose comment
	// lines of its header, on its method, and a table's or column's comment, such as COMMENT
	// ON TABLE gives it, on its model or field.
	DocComments bool

	// ModernIdioms writes the generated code in the idioms of current Go: a blank line
	// between every two top-level declarations, and iter.Seq2 as the type a stream method
	// returns. The default preset sets it; a preset that must reproduce code generated
	// before it existed byte for byte leaves it off.
	ModernIdioms bool

	// WrapErrors wraps every error a generated method returns with a label naming the
	// method, as fmt.Errorf("%s: %w", label, err) does, so a timeout or a syntax error says
	// which query met it while errors.Is and errors.As still find the error beneath, such as
	// sql.ErrNoRows.
	//
	// For a batch split across statements, the error also identifies the starting row of the
	// failed statement. Earlier rows have already been written.
	WrapErrors bool
}

// Interface controls the optional generated interface listing the method of every query,
// which the Queries type implements, so a caller can depend on the methods it uses and a
// test can stand in for the database.
type Interface struct {
	// Name is the interface's name, such as Querier: a Go identifier the generated package
	// declares no other way.
	Name string

	// Enabled reports whether the interface is generated.
	Enabled bool
}

// JSONCase is the case style the names in json tags are written in.
type JSONCase string

const (
	// JSONCaseAsWritten writes each name as the column is named.
	JSONCaseAsWritten JSONCase = ""

	// JSONCaseSnake writes each name in snake case, such as created_at.
	JSONCaseSnake JSONCase = "snake"

	// JSONCaseCamel writes each name in lower camel case, such as createdAt.
	JSONCaseCamel JSONCase = "camel"

	// JSONCasePascal writes each name in upper camel case, such as CreatedAt.
	JSONCasePascal JSONCase = "pascal"
)

// JSONTags shapes the json tags of generated fields. The zero value writes
// `json:"<column>"`, the column's name as it is written.
type JSONTags struct {
	// Case is the case style the names are written in.
	Case JSONCase

	// Omit leaves off the default json tag, except where Style.StructTags or a column's own
	// struct tags give one.
	Omit bool

	// OmitEmpty adds omitempty to default json tags, so encoding/json leaves out fields it
	// considers empty. Explicit tags from Style.StructTags or a column take precedence.
	OmitEmpty bool
}

// QueryNames controls the optional generated file holding a function that maps a query's
// SQL text to its name, for labelling telemetry.
type QueryNames struct {
	// File is the name of the generated file, such as query_names.go.
	File string

	// Function is the name of the generated function, such as QueryName.
	Function string

	// Enabled reports whether the file is generated.
	Enabled bool
}
