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

package pgx

import (
	"context"
	"fmt"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/internal/refusal"
	"seagull.sh/seagull/internal/gogen/generate"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// identDBTX is the generated DBTX interface identifier.
	identDBTX = "DBTX"

	// identError is the built-in error type identifier used in generated return signatures.
	identError = "error"

	// importPgx is the canonical pgx/v5 module import path.
	importPgx = "github.com/jackc/pgx/v5"

	// importPgconn is the canonical pgconn module import path.
	importPgconn = "github.com/jackc/pgx/v5/pgconn"
)

// Emitter implements emit.EmitterPort by generating Go source code targeting the pgx/v5
// runtime. All code generation uses go/ast node construction for deterministic,
// syntactically valid output.
type Emitter struct{}

var (
	_ emit.EmitterPort = (*Emitter)(nil)

	_ emit.TypeDefaulterPort = (*Emitter)(nil)
)

// New creates a new pgx code emitter.
//
// Returns *Emitter which is ready to emit Go source code.
func New() *Emitter {
	return &Emitter{}
}

// DefaultTypes returns the slots pgx reads natively where the built-in defaults are
// shaped for database/sql: pgx scans an interval into a time.Duration, which the
// database/sql default of string cannot hold.
//
// Takes info (*dialect.Facts) which is ignored: pgx reads intervals the same way for
// every dialect it serves.
//
// Returns sqlmodel.TypeSlots which holds the Interval slot.
func (*Emitter) DefaultTypes(_ *dialect.Facts) sqlmodel.TypeSlots {
	duration := sqlmodel.GoType{Composite: nil, Package: "time", Name: "Duration"}
	return sqlmodel.TypeSlots{
		Decimal:      sqlmodel.TypeSlot{},
		Money:        sqlmodel.TypeSlot{},
		BigInt:       sqlmodel.TypeSlot{},
		JSON:         sqlmodel.TypeSlot{},
		JSONScanInto: sqlmodel.TypeSlot{},
		Interval:     sqlmodel.TypeSlot{NotNull: duration, Nullable: sqlmodel.GoType{}},
		TimeOfDay:    sqlmodel.TypeSlot{},
		IPAddress:    sqlmodel.TypeSlot{},
		Map:          sqlmodel.TypeSlot{},
		Struct:       sqlmodel.TypeSlot{},
		Enum:         sqlmodel.TypeSlot{},
	}
}

// Emit generates every Go source file for one package targeting pgx/v5.
//
// It first refuses a request that cannot be emitted as it is, such as a nil one or one
// whose naming plan does not fit its queries, and then one Check finds an error problem
// with, for a caller that emits without checking. A slice parameter binds as one array in
// the dialect's array membership form, such as `= ANY($1)` for `IN ($1)`.
//
// Takes request (*emit.Request) which carries the package, schema, queries, type mappings
// and style.
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
	return generate.Package(ctx, request, pgxDriver(request))
}
