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

package pipeline

import (
	"context"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// operationEmit names the emitter's Emit call in a recovered panic.
	operationEmit = "emitter Emit"

	// operationCheck names the emitter's Check call in a recovered panic.
	operationCheck = "emitter Check"

	// operationDefaultTypes names the emitter's DefaultTypes call in a recovered panic.
	operationDefaultTypes = "emitter DefaultTypes"

	// operationBindsNullElements names the emitter's BindsNullElements call in a recovered
	// panic.
	operationBindsNullElements = "emitter BindsNullElements"

	// operationReserve names the emitter's naming reservation in a recovered panic.
	operationReserve = "emitter Reserve"

	// operationSchemaSource names the schema source's BuildCatalogue call in a recovered
	// panic.
	operationSchemaSource = "schema source BuildCatalogue"

	// operationDialectInfo names the dialect's Info call in a recovered panic.
	operationDialectInfo = "dialect Info"
)

// sourceCatalogue is what a schema source builds.
type sourceCatalogue struct {
	// catalogue holds the schema, or is nil when the source built none.
	catalogue *sqlmodel.Catalogue

	// diagnostics holds the diagnostics the source found while building it.
	diagnostics []diag.SourceError
}

// callSchemaSource asks the schema source for its catalogue, turning a panic into an
// error.
//
// Returns *sqlmodel.Catalogue which holds the schema the source built, or nil.
// Returns []diag.SourceError which holds the source's diagnostics.
// Returns error which is the source's own error, or one wrapping fault.ErrPanic when it
// panicked.
func (p *Pipeline) callSchemaSource(ctx context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	built, err := panicguard.RunValue(ctx, operationSchemaSource, func() (sourceCatalogue, error) {
		catalogue, diagnostics, err := p.buildCatalogue(ctx)
		return sourceCatalogue{catalogue: catalogue, diagnostics: diagnostics}, err
	})
	return built.catalogue, built.diagnostics, err
}

// callEmit asks the emitter for the Go source of a request, turning a panic into an
// error.
//
// Takes request (*emit.Request) which is the request to emit.
//
// Returns []sqlmodel.GeneratedFile which holds the generated files.
// Returns error which is the emitter's own error, or one wrapping fault.ErrPanic when it
// panicked.
func (p *Pipeline) callEmit(ctx context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	return panicguard.RunValue(ctx, operationEmit, func() ([]sqlmodel.GeneratedFile, error) {
		return p.emitter.Emit(ctx, request)
	})
}

// emitterSlots asks an emitter that implements emit.TypeDefaulterPort for the Go types it
// fills the preset's type slots with, turning a panic into an error.
//
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns sqlmodel.TypeSlots which holds the emitter's types, empty when it has none.
// Returns error which wraps fault.ErrPanic when the emitter panicked.
func (p *Pipeline) emitterSlots(ctx context.Context, info *dialect.Facts) (sqlmodel.TypeSlots, error) {
	defaulter, ok := p.emitter.(emit.TypeDefaulterPort)
	if !ok {
		return sqlmodel.TypeSlots{}, nil
	}
	return panicguard.RunValue(ctx, operationDefaultTypes, func() (sqlmodel.TypeSlots, error) {
		return defaulter.DefaultTypes(info), nil
	})
}

// callCheck asks an emitter's CheckerPort what it refuses in a request, turning a panic
// into an error.
//
// Takes checker (emit.CheckerPort) which is the emitter's checker.
// Takes request (*emit.Request) which is the request to check.
//
// Returns []emit.Problem which holds the problems the CheckerPort found.
// Returns error which wraps fault.ErrPanic when the CheckerPort panicked.
func callCheck(ctx context.Context, checker emit.CheckerPort, request *emit.Request) ([]emit.Problem, error) {
	return panicguard.RunValue(ctx, operationCheck, func() ([]emit.Problem, error) {
		return checker.Check(request), nil
	})
}

// bindsNullElements reports whether the emitter's driver binds NULL elements of a
// parameter's array, turning a panic into an error.
//
// Takes binder (emit.NullElementBinderPort) which is the emitter.
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns bool which is the emitter's answer.
// Returns error which wraps fault.ErrPanic when the emitter panicked.
func bindsNullElements(ctx context.Context, binder emit.NullElementBinderPort, info *dialect.Facts) (bool, error) {
	return panicguard.RunValue(ctx, operationBindsNullElements, func() (bool, error) {
		return binder.BindsNullElements(info), nil
	})
}

// dialectInfo asks the dialect for its Info, turning a panic into an error.
//
// Takes engine (dialect.DialectPort) which is the dialect.
//
// Returns dialect.Info which describes the dialect.
// Returns error which wraps fault.ErrPanic when the dialect panicked.
func dialectInfo(ctx context.Context, engine dialect.DialectPort) (dialect.Info, error) {
	return panicguard.RunValue(ctx, operationDialectInfo, func() (dialect.Info, error) {
		return engine.Info(), nil
	})
}

// reservationFor asks the emitter for the names its generated code uses, turning a panic
// into an error.
//
// Takes emitter (emit.EmitterPort) which is the emitter.
// Takes info (*dialect.Facts) which describes the dialect.
// Takes style (*emit.Style) which is the generated code's style.
//
// Returns emit.Reservation which holds the names.
// Returns error which wraps fault.ErrPanic when the emitter panicked.
func reservationFor(ctx context.Context, emitter emit.EmitterPort, info *dialect.Facts, style *emit.Style) (emit.Reservation, error) {
	return panicguard.RunValue(ctx, operationReserve, func() (emit.Reservation, error) {
		return emitkit.ReservationFor(emitter, info, style), nil
	})
}

// hostFailure reports a host call that panicked as a Q070 error that belongs to no one
// query.
//
// Takes err (error) which is the error the guarded call returned.
//
// Returns diag.SourceError which is the Q070 error.
func hostFailure(err error) diag.SourceError {
	return diagbuild.Unlocated(diag.CodeRecoveredPanic, diag.SeverityError, err.Error())
}
