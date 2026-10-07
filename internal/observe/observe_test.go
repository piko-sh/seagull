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

package observe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/observe"
)

type startRecord struct {
	stage observe.Stage
	attrs []slog.Attr
}

type recordingObserver struct {
	starts []startRecord
	ends   []error
}

type spanKey struct{}

func (recorder *recordingObserver) Start(ctx context.Context, stage observe.Stage, attrs ...slog.Attr) (context.Context, func(err error)) {
	recorder.starts = append(recorder.starts, startRecord{stage: stage, attrs: attrs})
	return context.WithValue(ctx, spanKey{}, stage), func(err error) {
		recorder.ends = append(recorder.ends, err)
	}
}

type faultyObserver struct {
	start func(ctx context.Context) (context.Context, func(err error))
}

func (observer faultyObserver) Start(ctx context.Context, _ observe.Stage, _ ...slog.Attr) (context.Context, func(err error)) {
	return observer.start(ctx)
}

func decodeRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any
	decoder := json.NewDecoder(logs)
	for decoder.More() {
		record := map[string]any{}
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

func startOne(ctx context.Context) {
	_, end := observe.From(ctx).Start(ctx, observe.StageGenerate)
	end(nil)
}

func TestDiscardReturnsContextUnchanged(t *testing.T) {
	t.Parallel()
	type key struct{}
	base := context.WithValue(t.Context(), key{}, "marker")
	ctx, end := observe.Discard().Start(base, observe.StageGenerate, slog.String("database", "main"))
	require.Equal(t, base, ctx)
	require.NotNil(t, end)
	end(nil)
	end(errors.New("ignored"))
}

func TestFromWithoutObserverReturnsDiscard(t *testing.T) {
	t.Parallel()
	var nilContext context.Context
	cases := []struct {
		ctx  context.Context
		name string
	}{
		{name: "nil context", ctx: nilContext},
		{name: "a context carrying nothing", ctx: t.Context()},
		{name: "unrelated value", ctx: context.WithValue(t.Context(), spanKey{}, 1)},
		{name: "nil observer", ctx: observe.ContextWithObserver(t.Context(), nil)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, observe.Discard(), observe.From(testCase.ctx))
		})
	}
}

func TestContextWithObserverRoundTrip(t *testing.T) {
	t.Parallel()
	var nilContext context.Context
	cases := []struct {
		parent context.Context
		name   string
	}{
		{name: "a parent carrying nothing", parent: t.Context()},
		{name: "nil parent starts from background", parent: nilContext},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			recorder := &recordingObserver{}
			ctx := observe.ContextWithObserver(testCase.parent, recorder)
			require.NotNil(t, ctx)
			startOne(ctx)
			require.Equal(t, []startRecord{{stage: observe.StageGenerate, attrs: nil}}, recorder.starts)
			require.Equal(t, []error{nil}, recorder.ends)
		})
	}
}

func TestContextWithObserverNilObserverReturnsContextUnchanged(t *testing.T) {
	t.Parallel()
	type key struct{}
	base := context.WithValue(t.Context(), key{}, "marker")
	ctx := observe.ContextWithObserver(base, nil)
	require.Equal(t, base, ctx)
}

func TestContextWithObserverInnermostWins(t *testing.T) {
	t.Parallel()
	outer := &recordingObserver{}
	inner := &recordingObserver{}
	ctx := observe.ContextWithObserver(observe.ContextWithObserver(t.Context(), outer), inner)
	startOne(ctx)
	require.Len(t, inner.starts, 1)
	require.Empty(t, outer.starts)
}

func TestObserverReceivesStageAttributesAndError(t *testing.T) {
	t.Parallel()
	recorder := &recordingObserver{}
	ctx := observe.ContextWithObserver(t.Context(), recorder)
	failure := errors.New("generation failed")

	stageContext, end := observe.From(ctx).Start(ctx, observe.StageAnalyse, slog.String("database", "main"))
	require.Equal(t, observe.StageAnalyse, stageContext.Value(spanKey{}))
	end(failure)

	require.Len(t, recorder.starts, 1)
	require.Equal(t, observe.StageAnalyse, recorder.starts[0].stage)
	require.Equal(t, []slog.Attr{slog.String("database", "main")}, recorder.starts[0].attrs)
	require.Equal(t, []error{failure}, recorder.ends)
}

func TestFromGuardsAFaultyObserverAndLogsItsPanicOnce(t *testing.T) {
	t.Parallel()
	type key struct{}
	var nilContext context.Context
	cases := []struct {
		start         func(ctx context.Context) (context.Context, func(err error))
		name          string
		wantOperation string
		wantPanic     string
	}{
		{name: "nil end function", start: func(ctx context.Context) (context.Context, func(err error)) { return ctx, nil }},
		{name: "nil context", start: func(context.Context) (context.Context, func(err error)) { return nilContext, func(error) {} }},
		{name: "nil context and end function", start: func(context.Context) (context.Context, func(err error)) { return nilContext, nil }},
		{
			name:          "panicking start",
			start:         func(context.Context) (context.Context, func(err error)) { panic("start failed") },
			wantOperation: "observer start of stage migrate.status",
			wantPanic:     "start failed",
		},
		{
			name: "panicking end function",
			start: func(ctx context.Context) (context.Context, func(err error)) {
				return ctx, func(error) { panic("end failed") }
			},
			wantOperation: "observer end of stage migrate.status",
			wantPanic:     "end failed",
		},
		{
			name:          "panic with nil",
			start:         func(context.Context) (context.Context, func(err error)) { panic(nil) },
			wantOperation: "observer start of stage migrate.status",
			wantPanic:     new(runtime.PanicNilError).Error(),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			base := context.WithValue(t.Context(), key{}, "marker")
			base = logging.ContextWithLogger(base, slog.New(slog.NewJSONHandler(&logs, nil)))
			ctx := observe.ContextWithObserver(base, faultyObserver{start: testCase.start})

			require.NotPanics(t, func() {
				stageContext, end := observe.From(ctx).Start(ctx, observe.StageMigrateStatus)
				require.NotNil(t, stageContext)
				require.Equal(t, "marker", stageContext.Value(key{}))
				require.NotNil(t, end)
				end(errors.New("stage failed"))
			})

			records := decodeRecords(t, &logs)
			if testCase.wantOperation == "" {
				require.Empty(t, records)
				return
			}
			require.Len(t, records, 1)
			record := records[0]
			require.Equal(t, slog.LevelWarn.String(), record[slog.LevelKey])
			require.Equal(t, "recovered a panic", record[slog.MessageKey])
			require.Equal(t, testCase.wantOperation, record["operation"])
			require.Equal(t, testCase.wantPanic, record["panic"])
			stack, ok := record["stack"].(string)
			require.True(t, ok, "stack is a string attribute")
			require.Contains(t, stack, "goroutine ")
		})
	}
}

func TestStageValues(t *testing.T) {
	t.Parallel()
	stages := map[observe.Stage]string{
		observe.StageGenerate:           "generate",
		observe.StageVet:                "vet",
		observe.StageCatalogue:          "generate.catalogue",
		observe.StageAnalyse:            "generate.analyse",
		observe.StageAnalyseQuery:       "generate.analyse.query",
		observe.StageResolveColumns:     "generate.resolve.columns",
		observe.StageResolveParameters:  "generate.resolve.params",
		observe.StageCatalogueMigration: "generate.catalogue.migration",
		observe.StageMigrateUp:          "migrate.up",
		observe.StageMigrateDown:        "migrate.down",
		observe.StageMigrateStatus:      "migrate.status",
		observe.StageMigrateValidate:    "migrate.validate",
		observe.StageMigrateForce:       "migrate.force",
		observe.StageMigrateRepairPlan:  "migrate.repair.plan",
		observe.StageMigrateRepair:      "migrate.repair",
		observe.StageSeedApply:          "seed.apply",
		observe.StageSeedStatus:         "seed.status",
		observe.StageSeedReseed:         "seed.reseed",
	}
	for stage, want := range stages {
		require.Equal(t, want, string(stage))
	}
}
