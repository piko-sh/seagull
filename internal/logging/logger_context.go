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

// ContextWithLogger returns a copy of ctx carrying logger.
//
// Seagull resolves the logger per call rather than capturing one at construction, so a
// host can scope a logger to a single generation or migration run. Passing a nil logger
// returns ctx unchanged, except that a nil context becomes context.Background().
//
// Takes logger (*slog.Logger) which Seagull should use for work performed under the
// returned context.
//
// Returns context.Context which carries the logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	if logger == nil {
		return ctx
	}

	return context.WithValue(ctx, loggerContextKey{}, logger)
}

// LoggerFrom returns the logger attached to ctx, falling back to the process default.
//
// Resolution happens at the call site rather than at construction so that a host calling
// slog.SetDefault() after Seagull starts is still honoured.
//
// Returns *slog.Logger which is never nil.
func LoggerFrom(ctx context.Context) *slog.Logger {
	logger, _ := loggerOf(ctx)
	return logger
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

// loggerOf returns the logger attached to ctx, falling back to the process default, and
// whether ctx could be read. A nil context, or one whose Value panics, as a faulty host
// context can, reads as unreadable and yields the process default.
//
// Returns logger (*slog.Logger) which is never nil.
// Returns readable (bool) which is false when ctx is nil or its Value panicked.
func loggerOf(ctx context.Context) (logger *slog.Logger, readable bool) {
	if ctx == nil {
		return slog.Default(), false
	}
	defer func() {
		if recover() != nil {
			logger, readable = slog.Default(), false
		}
	}()
	if attached, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok && attached != nil {
		return attached, true
	}
	return slog.Default(), true
}
