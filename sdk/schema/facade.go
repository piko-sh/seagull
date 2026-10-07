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

package schema

import (
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/schema"
)

const (
	// SeverityError marks a diagnostic that stops code generation, such as a schema object
	// the source cannot describe.
	SeverityError = diag.SeverityError

	// SeverityWarning marks a diagnostic that does not block generation, such as a column
	// type the source could only approximate.
	SeverityWarning = diag.SeverityWarning
)

// A schema source, the part of a dialect it reads types with, and the diagnostics it
// reports.
type (
	// SchemaSourcePort is the port to implement to give the generator its schema catalogue
	// from somewhere other than the migration files. schema_source_postgres and
	// schema_source_sqlite implement it for a live database; pass any implementation to
	// seagull.WithSchemaSource.
	//
	// Its one method, BuildCatalogue(ctx), returns the catalogue, the Catalogue of
	// sdk/dialect, the diagnostics found while building it as [SourceError] values, and an
	// error when it cannot be built: a schema problem belongs in the diagnostics, an
	// operational failure in the error. The generator calls it on every run; honour the
	// context's cancellation during slow reads. A nil catalogue without an error fails the
	// run with an error matching seagull.ErrNoSource, and a panic fails it with one matching
	// seagull.ErrPanic, its stack logged once at warning level.
	//
	// A view the source cannot type precisely may carry its definition, the SELECT text, in
	// its Definition field: the generator analyses it to type the view's columns.
	//
	// The source keeps ownership of the catalogue it returns. The generator copies it before
	// adding custom functions and column type overrides and never changes the original, so a
	// source may cache one catalogue and return it from every call, and concurrent runs, of
	// one generator or of several, may share it. The source must not change a catalogue it
	// has returned while a run may still be reading it.
	//
	// A source needs no kit beyond the model types sdk/dialect names;
	// docs/how-to/custom-schema-source.md walks through a complete one.
	SchemaSourcePort = schema.SchemaSourcePort

	// TypeNormaliserPort is the part of a dialect that turns the database's type names into
	// structured SQL types, which the built-in sources take beside the connection. Every
	// dialect satisfies it, so pass the one the generator uses, such as
	// dialect_engine_postgres.New().
	TypeNormaliserPort = dialect.TypeNormaliserPort

	// SourceError is a diagnostic a [SchemaSourcePort] reports beside its catalogue, with a
	// stable code, a severity, a message and, where one applies, a source position. It is
	// the same type as seagull.SourceError.
	SourceError = diag.SourceError
)
