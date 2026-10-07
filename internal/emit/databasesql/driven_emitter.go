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

package databasesql

import (
	"context"
	"fmt"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/emit/internal/refusal"
	"seagull.sh/seagull/internal/gogen/generate"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Emitter implements emit.EmitterPort by generating Go source code targeting the
// database/sql runtime. All code generation uses go/ast node construction for
// deterministic, syntactically valid output.
//
// The placeholders, identifier quoting and parameter limits of the generated code come
// from each request's dialect. How values are bound and scanned comes from the profile of
// the driver behind database/sql: by default the first of the dialect's drivers the
// emitter knows, or the driver WithDriver names.
type Emitter struct {
	// settings holds what the options chose.
	settings settings
}

var (
	_ emit.EmitterPort = (*Emitter)(nil)

	_ emit.TypeDefaulterPort = (*Emitter)(nil)
)

// New creates a database/sql code emitter.
//
// Takes options (...Option) which configure the emitter.
//
// Returns *Emitter which is ready to emit Go source code.
func New(options ...Option) *Emitter {
	emitter := &Emitter{settings: settings{prepared: nil, driver: ""}}
	for _, option := range options {
		option(&emitter.settings)
	}
	return emitter
}

// Emit generates every Go source file for one package targeting database/sql.
//
// It first refuses a request that cannot be emitted as it is, such as a nil one or one
// whose naming plan does not fit its queries, and then one Check finds an error problem
// with, for a caller that emits without checking, and by default it leaves out the
// prepared-statement cache for a driver that prepares only INSERT statements.
//
// Takes request (*emit.Request) which carries the package, schema, queries, type
// mappings, dialect and style.
//
// Returns []sqlmodel.GeneratedFile which holds the generated files.
// Returns error when the request is refused, or Check finds an error problem, either
// wrapping emit.ErrRefused, or when code emission fails.
func (emitter *Emitter) Emit(ctx context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	if err := refusal.Invalid(generate.ValidateRequest(request)); err != nil {
		return nil, fmt.Errorf("emitting queries: %w", err)
	}
	if err := refusal.Error(emitter.Check(request)); err != nil {
		return nil, fmt.Errorf("emitting queries for %s: %w", request.Package, err)
	}
	profile := profileFor(request.Dialect.Drivers, emitter.settings.driver)
	if bindsNamedValues(&profile, request.Dialect.Placeholders) {
		copied := *request
		copied.Queries = chvalues.ArrayLists(request.Queries)
		request = &copied
	}
	prepared := !profile.preparesOnlyInserts && (emitter.settings.prepared == nil || *emitter.settings.prepared)
	return generate.Package(ctx, request, newSQLDriver(request, &profile, prepared).driver())
}

// DefaultTypes returns the slots the driver behind database/sql needs where the built-in
// defaults do not round-trip, from the driver's profile.
//
// Takes info (*dialect.Facts) which describes the dialect the queries are analysed with,
// whose drivers choose the profile unless WithDriver named one.
//
// Returns sqlmodel.TypeSlots which holds the driver's slots, or zero slots.
func (emitter *Emitter) DefaultTypes(info *dialect.Facts) sqlmodel.TypeSlots {
	return profileFor(info.Drivers, emitter.settings.driver).types
}

// Option configures an Emitter.
type Option func(*settings)

// settings holds what an Emitter's options choose.
type settings struct {
	// prepared is the choice WithPrepared made, or nil when no option made one.
	prepared *bool

	// driver is the module of the driver the emitter generates for; empty means the first
	// known driver of each request's dialect.
	driver string
}

// WithDriver names the driver behind database/sql the generated code runs on, by the
// import path of its Go module, such as github.com/jackc/pgx/v5, instead of the first of
// the dialect's drivers the emitter knows. A driver the emitter does not know gets the
// general profile, which keeps the built-in type defaults.
//
// Takes driver (string) which is the driver's module path.
//
// Returns Option which fixes the driver.
func WithDriver(driver string) Option {
	return func(chosen *settings) {
		chosen.driver = driver
	}
}

// WithPrepared sets whether the emitter generates prepared.go, a PreparedDBTX that
// prepares every static query once and reuses the statements.
//
// It is on by default, except for a driver that prepares only some statements, which gets
// no cache; asking for it there with WithPrepared(true) is a problem Check reports.
// PreparesStatements says which drivers can.
//
// Takes enabled (bool) which is false to leave the prepared-statement cache out.
//
// Returns Option which sets the choice.
func WithPrepared(enabled bool) Option {
	return func(chosen *settings) {
		chosen.prepared = &enabled
	}
}
