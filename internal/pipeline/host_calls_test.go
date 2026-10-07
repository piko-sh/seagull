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
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

const (
	panicsInEmit              = "Emit"
	panicsInCheck             = "Check"
	panicsInDefaultTypes      = "DefaultTypes"
	panicsInBindsNullElements = "BindsNullElements"
	panicsInReserve           = "Reserve"
)

type panickingEmitter struct {
	fakedialect.Emitter
	panicsIn string
}

func (e *panickingEmitter) Emit(ctx context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	if e.panicsIn == panicsInEmit {
		panic("boom")
	}
	return e.Emitter.Emit(ctx, request)
}

func (e *panickingEmitter) Check(*emit.Request) []emit.Problem {
	if e.panicsIn == panicsInCheck {
		panic("boom")
	}
	return nil
}

func (e *panickingEmitter) DefaultTypes(*dialect.Facts) sqlmodel.TypeSlots {
	if e.panicsIn == panicsInDefaultTypes {
		panic("boom")
	}
	return sqlmodel.TypeSlots{}
}

func (e *panickingEmitter) BindsNullElements(*dialect.Facts) bool {
	if e.panicsIn == panicsInBindsNullElements {
		panic("boom")
	}
	return true
}

func (e *panickingEmitter) Reserve(info *dialect.Facts, style *emit.Style) emit.Reservation {
	if e.panicsIn == panicsInReserve {
		panic("boom")
	}
	return emitkit.ReservationFor(&e.Emitter, info, style)
}

var (
	_ emit.CheckerPort           = (*panickingEmitter)(nil)
	_ emit.TypeDefaulterPort     = (*panickingEmitter)(nil)
	_ emit.NullElementBinderPort = (*panickingEmitter)(nil)
	_ emit.ReserverPort          = (*panickingEmitter)(nil)
)

func recordingLogger() (*slog.Logger, *bytes.Buffer) {
	var logs bytes.Buffer
	return slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})), &logs
}

func queryingEngine() *fakedialect.Engine {
	return &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Raw: nil, Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{}, nil
		},
	}
}

func oneQueryRequest() testRequest {
	return testRequest{
		packageName:    "testdb",
		queries:        fstest.MapFS{"queries/q.sql": &fstest.MapFile{Data: []byte("-- sgl.query(Touch, exec)\nUPDATE t SET a = 1;\n")}},
		queryDirectory: "queries",
	}
}

func TestGenerateRecoversAPanicInAnEmitterCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		panicsIn       string
		wantErr        string
		wantDiagnostic string
	}{
		{name: "Emit is returned as an error naming the package", panicsIn: panicsInEmit, wantErr: "emitting package testdb: emitter Emit: panic: boom"},
		{name: "Check is reported as a Q070 error", panicsIn: panicsInCheck, wantDiagnostic: "emitter Check: panic: boom"},
		{name: "DefaultTypes is reported as a Q070 error", panicsIn: panicsInDefaultTypes, wantDiagnostic: "emitter DefaultTypes: panic: boom"},
		{name: "BindsNullElements is returned as an error", panicsIn: panicsInBindsNullElements, wantErr: "analysing queries: emitter BindsNullElements: panic: boom"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			logger, logs := recordingLogger()
			emitter := &panickingEmitter{panicsIn: testCase.panicsIn}
			generator, err := New(Config{Dialect: queryingEngine(), Emitter: emitter, BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue, Logger: logger})
			require.NoError(t, err)

			generation, err := generator.Generate(t.Context(), oneQueryRequest())
			assert.Equal(t, 1, strings.Count(logs.String(), "recovered a panic"), logs.String())
			if testCase.wantErr != "" {
				require.ErrorIs(t, err, fault.ErrPanic)
				assert.EqualError(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Empty(t, generation.Files)
			assert.Empty(t, emitter.Requests)
			require.Len(t, generation.Diagnostics, 1)
			assert.Equal(t, diag.CodeRecoveredPanic, generation.Diagnostics[0].Code)
			assert.Equal(t, diag.SeverityError, generation.Diagnostics[0].Severity)
			assert.Equal(t, testCase.wantDiagnostic, generation.Diagnostics[0].Message)
		})
	}
}

func TestVetReportsAPanicInTheEmittersCheckerAsQ070(t *testing.T) {
	t.Parallel()

	logger, logs := recordingLogger()
	generator, err := New(Config{Dialect: queryingEngine(), Emitter: &panickingEmitter{panicsIn: panicsInCheck},
		BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue, Logger: logger})
	require.NoError(t, err)

	queries, diagnostics, err := generator.Vet(t.Context(), oneQueryRequest())
	require.NoError(t, err)
	assert.Len(t, queries, 1)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, diag.CodeRecoveredPanic, diagnostics[0].Code)
	assert.Equal(t, 1, strings.Count(logs.String(), "recovered a panic"))
}

func TestASchemaSourceThatPanicsIsReturnedAsAnError(t *testing.T) {
	t.Parallel()

	source := &mockCatalogueProvider{buildCatalogue: func(context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
		panic(errors.New("source exploded"))
	}}
	generator, err := New(Config{Dialect: queryingEngine(), Emitter: &fakedialect.Emitter{}, BuildCatalogue: source.BuildCatalogue})
	require.NoError(t, err)

	tests := []struct {
		name string
		run  func(ctx context.Context) error
	}{
		{name: "Generate", run: func(ctx context.Context) error {
			_, runErr := generator.Generate(ctx, oneQueryRequest())
			return runErr
		}},
		{name: "Vet", run: func(ctx context.Context) error {
			_, _, runErr := generator.Vet(ctx, oneQueryRequest())
			return runErr
		}},
		{name: "Catalogue", run: func(ctx context.Context) error {
			_, _, runErr := generator.Catalogue(ctx, oneQueryRequest())
			return runErr
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			logger, _ := recordingLogger()
			err := testCase.run(logging.ContextWithLogger(t.Context(), logger))
			require.ErrorIs(t, err, fault.ErrPanic)
			assert.ErrorContains(t, err, "building catalogue: schema source BuildCatalogue: panic: source exploded")
		})
	}
}

func TestEveryRunRecoversAPanicThatEscapesADialectCall(t *testing.T) {
	t.Parallel()

	engine := queryingEngine()
	engine.BuiltinTypesFunc = func() *ast.TypeCatalogue {
		panic("the dialect broke")
	}
	generator, err := New(Config{Dialect: engine, Emitter: &fakedialect.Emitter{}, BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue})
	require.NoError(t, err)

	tests := []struct {
		name      string
		operation string
		run       func(ctx context.Context) error
	}{
		{name: "Generate", operation: operationGenerate, run: func(ctx context.Context) error {
			_, runErr := generator.Generate(ctx, oneQueryRequest())
			return runErr
		}},
		{name: "GenerateDetailed", operation: operationGenerate, run: func(ctx context.Context) error {
			_, runErr := generator.GenerateDetailed(ctx, oneQueryRequest())
			return runErr
		}},
		{name: "Vet", operation: operationVet, run: func(ctx context.Context) error {
			_, _, runErr := generator.Vet(ctx, oneQueryRequest())
			return runErr
		}},
		{name: "Catalogue", operation: operationCatalogue, run: func(ctx context.Context) error {
			_, _, runErr := generator.Catalogue(ctx, oneQueryRequest())
			return runErr
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			logger, logs := recordingLogger()
			err := testCase.run(logging.ContextWithLogger(t.Context(), logger))
			require.ErrorIs(t, err, fault.ErrPanic)
			assert.EqualError(t, err, testCase.operation+": panic: the dialect broke")
			assert.Equal(t, 1, strings.Count(logs.String(), "recovered a panic"))
		})
	}
}

func TestNewRecoversAPanicInTheDialectOrTheEmitter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name:    "the dialect's Info",
			config:  Config{Dialect: &fakedialect.Engine{InfoFunc: func() dialect.Info { panic("no info") }}, Emitter: &fakedialect.Emitter{}},
			wantErr: "dialect Info: panic: no info",
		},
		{
			name:    "the emitter's naming reservation",
			config:  Config{Dialect: &fakedialect.Engine{}, Emitter: &panickingEmitter{panicsIn: panicsInReserve}},
			wantErr: "emitter Reserve: panic: boom",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			logger, logs := recordingLogger()
			testCase.config.Logger = logger
			generator, err := New(testCase.config)
			require.ErrorIs(t, err, fault.ErrPanic)
			assert.EqualError(t, err, testCase.wantErr)
			assert.Nil(t, generator)
			assert.Equal(t, 1, strings.Count(logs.String(), "recovered a panic"))
		})
	}
}
