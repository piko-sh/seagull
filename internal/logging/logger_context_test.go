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

package logging_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/logging"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWithLoggerBindsTheLogger(t *testing.T) {
	t.Parallel()
	var nilContext context.Context
	outer := newTestLogger()
	inner := newTestLogger()
	cases := []struct {
		ctx  context.Context
		bind *slog.Logger
		want *slog.Logger
		name string
	}{
		{name: "round trip", ctx: t.Context(), bind: inner, want: inner},
		{name: "nil context starts from background", ctx: nilContext, bind: inner, want: inner},
		{name: "innermost wins", ctx: logging.WithLogger(t.Context(), outer), bind: inner, want: inner},
		{name: "nil logger keeps the bound one", ctx: logging.WithLogger(t.Context(), outer), bind: nil, want: outer},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx := logging.WithLogger(testCase.ctx, testCase.bind)
			require.NotNil(t, ctx)
			_, got := logging.From(ctx)
			assert.Same(t, testCase.want, got)
		})
	}
}

func TestWithLoggerKeepsOtherValues(t *testing.T) {
	t.Parallel()
	type key struct{}
	base := context.WithValue(t.Context(), key{}, "marker")
	cases := []struct {
		logger *slog.Logger
		name   string
	}{
		{name: "a logger", logger: newTestLogger()},
		{name: "nil logger", logger: nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "marker", logging.WithLogger(base, testCase.logger).Value(key{}))
		})
	}
	assert.Equal(t, base, logging.WithLogger(base, nil), "a nil logger returns ctx unchanged")
}

func TestWithFallbackLoggerYieldsToALoggerAlreadyInTheContext(t *testing.T) {
	t.Parallel()
	var nilContext context.Context
	bound := newTestLogger()
	fallback := newTestLogger()
	cases := []struct {
		ctx      context.Context
		fallback *slog.Logger
		want     *slog.Logger
		name     string
	}{
		{name: "a bound logger wins", ctx: logging.WithLogger(t.Context(), bound), fallback: fallback, want: bound},
		{name: "the fallback is used when ctx has none", ctx: t.Context(), fallback: fallback, want: fallback},
		{name: "nil context takes the fallback", ctx: nilContext, fallback: fallback, want: fallback},
		{name: "nil fallback keeps the bound logger", ctx: logging.WithLogger(t.Context(), bound), fallback: nil, want: bound},
		{name: "nil fallback and no logger resolve the default", ctx: t.Context(), fallback: nil, want: slog.Default()},
		{name: "an unreadable context takes the fallback", ctx: unreadableContext{Context: t.Context()}, fallback: fallback, want: fallback},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var ctx context.Context
			require.NotPanics(t, func() { ctx = logging.WithFallbackLogger(testCase.ctx, testCase.fallback) })
			require.NotNil(t, ctx)
			_, got := logging.From(ctx)
			assert.Same(t, testCase.want, got)
		})
	}

	ctx := logging.WithLogger(t.Context(), bound)
	assert.Equal(t, ctx, logging.WithFallbackLogger(ctx, fallback), "a bound logger is kept and nothing is wrapped")
}

func TestFromBindsDefaultIntoReturnedContext(t *testing.T) {
	t.Parallel()
	var nilContext context.Context
	cases := []struct {
		ctx  context.Context
		name string
	}{
		{name: "nil context", ctx: nilContext},
		{name: "background", ctx: context.Background()},
		{name: "unrelated value", ctx: context.WithValue(t.Context(), struct{}{}, 1)},
		{name: "a context whose Value panics", ctx: unreadableContext{Context: t.Context()}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var ctx context.Context
			var logger *slog.Logger
			require.NotPanics(t, func() { ctx, logger = logging.From(testCase.ctx) })
			require.NotNil(t, ctx)
			assert.Same(t, slog.Default(), logger)
			var again *slog.Logger
			require.NotPanics(t, func() { _, again = logging.From(ctx) })
			assert.Same(t, logger, again, "the bound logger is shared down the chain")
		})
	}
}

func TestFromReturnsABoundContextUnchanged(t *testing.T) {
	t.Parallel()
	logger := newTestLogger()
	ctx := logging.WithLogger(t.Context(), logger)
	gotContext, got := logging.From(ctx)
	assert.Same(t, logger, got)
	assert.Equal(t, ctx, gotContext)
}

func TestFromAllocatesNothingForABoundContext(t *testing.T) {
	ctx := logging.WithLogger(t.Context(), newTestLogger())
	allocations := testing.AllocsPerRun(100, func() {
		_, _ = logging.From(ctx)
	})
	assert.Zero(t, allocations)
}

func TestFromKeepsBoundLoggerAfterDefaultChanges(t *testing.T) {
	ctx, first := logging.From(t.Context())
	previous := slog.Default()
	replacement := newTestLogger()
	slog.SetDefault(replacement)
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, again := logging.From(ctx)
	assert.Same(t, first, again, "a chain keeps the logger it resolved first")

	_, fresh := logging.From(t.Context())
	assert.Same(t, replacement, fresh, "a new chain honours the changed default")
}

func TestErrAttr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want slog.Attr
		name string
	}{
		{name: "nil error", err: nil, want: slog.String("error", "<nil>")},
		{name: "plain error", err: errors.New("boom"), want: slog.String("error", "boom")},
		{name: "wrapped error", err: errors.Join(errors.New("first"), errors.New("second")), want: slog.String("error", "first\nsecond")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := logging.ErrAttr(testCase.err)
			require.True(t, testCase.want.Equal(got), "want %v, got %v", testCase.want, got)
			assert.Equal(t, "error", got.Key)
		})
	}
}
