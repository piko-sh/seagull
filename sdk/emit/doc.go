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

// Package emit is the public contract for code emitters. An emitter turns the queries
// Seagull has analysed into Go source written against one database driver API, and the
// generator calls it once for each package it writes.
//
// # Ports
//
// [EmitterPort] is the port to implement. Its one method, Emit, takes a [Request] and
// returns the package's files as [GeneratedFile] values. It stays a single method so the
// request can gain fields without breaking an implementation.
//
// An emitter may also implement optional ports, which the generator finds with a type
// assertion:
//
//   - [TypeDefaulterPort] supplies the Go types its driver reads natively, ranked above
//     the built-in defaults and below configured type slots and overrides.
//   - [ReserverPort] lists the names its generated code declares besides the planned
//     ones, so the naming plan never chooses one of them.
//   - [NullElementBinderPort] says whether its driver binds a NULL element of an array or
//     list parameter, or a NULL value of a map parameter.
//   - [CheckerPort] refuses, before Emit runs, what its driver cannot serve, reported as
//     a diagnostic at the query rather than as an error.
//
// # The request
//
// A [Request] describes one generated package: its name, the catalogue its models come
// from, the analysed queries, the [GoTypeResolver] that maps each SQL type to a Go type,
// the dialect's facts, the [Style] of the generated code and the [Naming] plan, which
// fixes every name the code declares so that every emitter writes the same names.
//
// Each [AnalysedQuery] holds its [QueryCommand], its typed [QueryParameter] and
// [OutputColumn] values, and its executable SQL as a [SQLTemplate] of [SQLSegment] parts,
// which an emitter renders in the placeholder form its driver executes. A [GoType] names
// a Go type by import path and name, or describes a map, a struct or an instance of a
// generic type. [TypeSlots] are how a driver or a configuration chooses Go types. An
// emitter reports what it cannot serve as a [Problem] with an [ErrorSeverity] and the
// code [CodeEmitterUnsupported].
//
// The dialect's facts, Request.Dialect, and the catalogue, Request.Catalogue, are the
// Facts and Catalogue of seagull.sh/seagull/sdk/dialect, which also names the SQL types
// the queries hold.
//
// # Choosing a driver provider
//
// Seagull ships one emitter for each driver API it writes code against. Pass one to
// seagull.WithEmitter, or choose it with go.driver in sgl.yaml:
//
//   - emit_driver_databasesql writes code against the standard database/sql interfaces,
//     so it works with every dialect and any database/sql driver. It is the default when
//     no emitter is given, and the portable choice.
//   - emit_driver_pgx writes code against pgx v5's native interfaces, for a dialect that
//     runs on pgx, such as PostgreSQL, CockroachDB or TimescaleDB. Pick it when the
//     application talks to pgx directly, for COPY, pgx batches and array parameters.
//   - emit_driver_clickhouse writes code against clickhouse-go v2's native interface, for
//     ClickHouse. Pick it when the application uses the native driver, for its columnar
//     batches.
//
// # Writing and checking an emitter
//
// A driver API none of them covers needs an EmitterPort of its own. emit_kit holds the
// building blocks the built-in emitters share: rendering a SQLTemplate for a driver,
// checking batch shapes and type depths, the default reservation and the row template a
// batch repeats. emit_testkit holds the conformance suite: its Run drives an emitter
// through the generator with every dialect a Suite names, and an invented one besides,
// and checks that the generated packages build and behave as every emitter's must.
//
// # Usage
//
//	generator, err := seagull.New(engine, seagull.WithEmitter(emit_driver_pgx.New()))
//	if err != nil {
//	    return err
//	}
//
// An emitter of one's own implements Emit, and optionally the other ports:
//
//	type Emitter struct{}
//
//	func (*Emitter) Emit(ctx context.Context, request *emit.Request) ([]emit.GeneratedFile, error) {
//	    // Render each query's Template and write the package's files.
//	}
package emit
