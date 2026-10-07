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

package seagull

import (
	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/schema"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// JSONCaseAsWritten writes each json tag name as the column is named.
	JSONCaseAsWritten = emit.JSONCaseAsWritten

	// JSONCaseSnake writes each json tag name in snake case, such as created_at.
	JSONCaseSnake = emit.JSONCaseSnake

	// JSONCaseCamel writes each json tag name in lower camel case, such as createdAt.
	JSONCaseCamel = emit.JSONCaseCamel

	// JSONCasePascal writes each json tag name in upper camel case, such as CreatedAt.
	JSONCasePascal = emit.JSONCasePascal
)

// Input names the SQL one run reads.
type Input = app.Input

// Result is the outcome of Generate.
type Result = app.Result

// DetailedResult is the outcome of GenerateDetailed: generated files, the first checker
// report and the schema catalogue from one analysis. Each result owns its mutable data
// independently of other calls and the schema source.
type DetailedResult = app.DetailedResult

// Report is the outcome of Vet: the analysed queries and every diagnostic, without
// generated code.
type Report = app.Report

// Option configures a Generator.
type Option = app.Option

// WriteOption configures WriteFiles.
type WriteOption = app.WriteOption

// Preset bundles the settings that give generated code, diagnostics and directives their
// vocabulary: the directive prefixes, the generated-file header, the helper and error
// prefixes, the type slots and the optional query-names file.
//
// A Preset is plain data, so an embedder builds its own, starting from DefaultPreset and
// changing what it needs. Validate reports whether it can be used.
type Preset = app.Preset

// Directives names the directive prefixes recognised in SQL comments: the canonical
// prefix that diagnostics render, the aliases accepted as synonyms, the foreign prefixes
// reported but never parsed, and whether an alias draws a hint.
type Directives = app.Directives

// DiagnosticsError reports that a run produced diagnostics of error severity. Result.Err
// and Report.Err return one; it matches ErrDiagnostics with errors.Is, and ErrParse too
// when SQL could not be parsed (an error-severity Q010).
type DiagnosticsError = app.DiagnosticsError

// SchemaSourcePort is the port a schema source implements to build the schema catalogue
// from somewhere other than the migration files; WithSchemaSource sets one. It is the
// SchemaSourcePort of sdk/schema, whose providers schema_source_postgres and
// schema_source_sqlite read the catalogue from a live database.
//
// A view the source cannot type precisely may carry its definition, the SELECT text, in
// the Definition field of the View (a type of sdk/dialect): the generator analyses it to
// type the view's columns.
//
// The source keeps ownership of the catalogue it returns. The generator copies it before
// adding custom functions and column type overrides and never changes the original, so a
// source may cache one catalogue and return it from every call, and concurrent runs, of
// one generator or of several, may share it. The source must not change a catalogue it
// has returned while a run may still be reading it.
type SchemaSourcePort = schema.SchemaSourcePort

// TypeOverride maps a SQL type, or one column, to a Go type, ahead of the preset's type
// slots and the emitter's and built-in defaults.
//
// Set SQLTypeName to override every column of that type, or Column ("table.column" or
// "schema.table.column") to override one column as a migration go_type directive does; a
// go_type directive on that column still wins.
type TypeOverride = sqlmodel.TypeOverride

// CustomFunction declares a SQL function the dialect does not know, such as one from a
// loadable extension, so queries and views that call it can be analysed. New rejects a
// declaration it cannot use with ErrInvalidConfig.
type CustomFunction = sqlmodel.CustomFunctionConfig

// TypeSlots selects Go types for SQL values that need custom representations or driver
// support. A zero slot keeps the default type.
type TypeSlots = sqlmodel.TypeSlots

// Slot names the Go types one category of SQL values maps to, for NOT NULL and nullable
// values. The zero value keeps the default.
type Slot = sqlmodel.TypeSlot

// GoType names a Go type by its import path and its name, such as {Package:
// "github.com/google/uuid", Name: "UUID"} or {Name: "*int64"} for a built-in type, for
// the NOT NULL and nullable halves of a Slot.
type GoType = sqlmodel.GoType

// QueryNames controls the optional generated file holding a function that maps a query's
// SQL text to its name, for labelling telemetry.
type QueryNames = emit.QueryNames

// Interface controls the optional generated interface listing the method of every query,
// which Queries implements.
type Interface = emit.Interface

// JSONTags shapes the json tags of generated fields.
type JSONTags = emit.JSONTags

// JSONCase is the case style the names in json tags are written in: JSONCaseAsWritten,
// JSONCaseSnake, JSONCaseCamel or JSONCasePascal.
type JSONCase = emit.JSONCase
