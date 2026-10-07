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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/logging"
)

type panickingStringer struct{}

func (panickingStringer) String() string {
	panic("String failed")
}

type unreadableContext struct {
	context.Context
}

func (unreadableContext) Value(any) any {
	panic("the context cannot be read")
}

func panicAndLog(ctx context.Context, operation string, value any) {
	defer func() {
		logging.LogRecoveredPanic(ctx, operation, recover())
	}()
	panic(value)
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

func TestLogRecoveredPanicWritesOneWarningThroughTheContextLogger(t *testing.T) {
	t.Parallel()
	cases := []struct {
		recovered any
		name      string
		operation string
		wantPanic string
	}{
		{name: "string value", operation: "analysing query", recovered: "boom", wantPanic: "boom"},
		{name: "error value", operation: "running hook", recovered: errors.New("disk full"), wantPanic: "disk full"},
		{name: "integer value", operation: "parsing statement", recovered: 42, wantPanic: "42"},
		{
			name:      "value whose String method panics",
			operation: "formatting",
			recovered: panickingStringer{},
			wantPanic: "%!v(PANIC=String method: String failed)",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))

			panicAndLog(ctx, testCase.operation, testCase.recovered)

			records := decodeRecords(t, &logs)
			require.Len(t, records, 1)
			record := records[0]
			assert.Equal(t, slog.LevelWarn.String(), record[slog.LevelKey])
			assert.Equal(t, "recovered a panic", record[slog.MessageKey])
			assert.Equal(t, testCase.operation, record["operation"])
			assert.Equal(t, testCase.wantPanic, record["panic"])
			stack, ok := record["stack"].(string)
			require.True(t, ok, "stack is a string attribute")
			assert.Contains(t, stack, "goroutine ")
			assert.Contains(t, stack, "panic(")
			assert.Contains(t, stack, "logging_test.panicAndLog")
		})
	}
}

func TestAContextThatCannotBeReadFallsBackToTheDefaultLogger(t *testing.T) {
	testCases := []struct {
		ctx  context.Context
		name string
	}{
		{name: "a context whose Value panics", ctx: unreadableContext{Context: t.Context()}},
		{name: "a nil context", ctx: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var logger *slog.Logger
			require.NotPanics(t, func() { logger = logging.LoggerFrom(testCase.ctx) })
			assert.Same(t, slog.Default(), logger)
			require.NotPanics(t, func() { panicAndLog(testCase.ctx, "probe", "boom") })
		})
	}
}
