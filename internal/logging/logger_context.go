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

package logging

import (
	"context"
	"log/slog"
)

const (
	// LevelTrace is the most detailed level, used for per-statement analyser tracing. It
	// matches the numeric trace level of common slog handlers, so a host that already
	// renders a TRACE level shows these records under it.
	LevelTrace = slog.Level(-8)

	// LevelInternal sits between LevelTrace and slog.LevelDebug and carries detail that is
	// only useful when diagnosing Seagull itself.
	LevelInternal = slog.Level(-6)
)

// loggerContextKey keys the logger a host attaches to a context.
type loggerContextKey struct{}

// WithLogger returns a copy of ctx carrying logger.
//
// Passing a nil logger returns ctx unchanged, except that a nil context becomes
// context.Background().
//
// Takes logger (*slog.Logger) which Seagull should use for work performed under the
// returned context.
//
// Returns context.Context which carries the logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	if logger == nil {
		return ctx
	}

	return context.WithValue(ctx, loggerContextKey{}, logger)
}

// WithFallbackLogger returns ctx carrying logger only when ctx carries no logger yet. A
// logger the caller bound into ctx wins over the fallback, so a host's per-run logger
// overrides one configured for every run.
//
// Takes logger (*slog.Logger) which is used when ctx carries none. Nil returns ctx
// unchanged.
//
// Returns context.Context which carries a logger when either ctx or logger had one.
func WithFallbackLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		return ctx
	}
	if bound, _ := boundLogger(ctx); bound != nil {
		return ctx
	}
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

// From returns the logger attached to ctx, together with a context that carries it.
//
// When ctx carries no logger, the process default is bound into the returned context, so
// every call further down the same chain resolves to one logger without looking the
// default up again. A context whose Value panics also gets the default bound, which
// shields later lookups from the panic.
//
// Returns context.Context which carries the resolved logger and is never nil.
// Returns *slog.Logger which is never nil.
func From(ctx context.Context) (context.Context, *slog.Logger) {
	if ctx == nil {
		ctx = context.Background()
	}

	if logger, _ := boundLogger(ctx); logger != nil {
		return ctx, logger
	}

	logger := slog.Default()

	return context.WithValue(ctx, loggerContextKey{}, logger), logger
}

// ErrAttr builds the structured attribute Seagull uses to report an error.
//
// Takes err (error) which may be nil.
//
// Returns slog.Attr which holds the error message under the key "error".
func ErrAttr(err error) slog.Attr {
	if err == nil {
		return slog.String("error", "<nil>")
	}

	return slog.String("error", err.Error())
}

// boundLogger returns the logger attached to ctx, or nil when there is none. A nil
// context, or one whose Value panics as a faulty host context can, reads as unreadable.
//
// Returns logger (*slog.Logger) which is nil when ctx carries none or cannot be read.
// Returns readable (bool) which is false when ctx is nil or its Value panicked.
func boundLogger(ctx context.Context) (logger *slog.Logger, readable bool) {
	if ctx == nil {
		return nil, false
	}
	defer func() {
		if recover() != nil {
			logger, readable = nil, false
		}
	}()
	if attached, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok {
		return attached, true
	}
	return nil, true
}
