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

package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/logging"
)

func TestCallHookReturnsAStackFreeErrorAndLogsThePanicOnce(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("hook failed")

	tests := []struct {
		operation func() error
		wantError error
		wantPanic any
		name      string
		wantText  string
		wantLogs  int
	}{
		{name: "success", operation: func() error { return nil }},
		{name: "hook error", operation: func() error { return sentinel }, wantError: sentinel, wantText: "hook failed"},
		{name: "panic with a value", operation: func() error { panic("boom") }, wantPanic: "boom", wantText: "before-run hook panicked: boom", wantLogs: 1},
		{
			name:      "panic with an error",
			operation: func() error { panic(sentinel) },
			wantError: sentinel,
			wantPanic: sentinel,
			wantText:  "before-run hook panicked: hook failed",
			wantLogs:  1,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))

			err := callHook(ctx, "before-run hook", testCase.operation)

			records := decodeLogRecords(t, &logs)
			require.Len(t, records, testCase.wantLogs)
			for _, record := range records {
				assert.Equal(t, "WARN", record["level"])
				assert.Equal(t, "before-run hook", record["operation"])
				assert.Contains(t, record["stack"], "callHook")
			}
			if testCase.wantText == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, testCase.wantText)
			if testCase.wantError != nil {
				assert.ErrorIs(t, err, testCase.wantError)
			}
			if testCase.wantPanic == nil {
				_, isPanic := errors.AsType[*HookPanicError](err)
				assert.False(t, isPanic)
				assert.NotErrorIs(t, err, ErrPanic)
				return
			}
			var panicError *HookPanicError
			require.ErrorAs(t, err, &panicError)
			assert.Equal(t, &HookPanicError{Value: testCase.wantPanic, Hook: "before-run hook"}, panicError)
			assert.ErrorIs(t, err, ErrPanic)
		})
	}
}

func decodeLogRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
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

func TestHookPanicStopsTheRun(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t (id int);")}}
	executed := false
	executor := &mockMigrationExecutor{
		executeMigrationFn: func(context.Context, MigrationRecord, MigrationDirection, bool) error {
			executed = true
			return nil
		},
	}
	hook := WithBeforeMigration(func(context.Context, MigrationHookContext) error { panic("hook broke") })
	migrator, err := New(executor, files, "migrations", hook)
	require.NoError(t, err)

	count, err := migrator.Up(t.Context())

	var panicError *HookPanicError
	require.ErrorAs(t, err, &panicError)
	assert.Equal(t, "migration before-hook", panicError.Hook)
	assert.Equal(t, "hook broke", panicError.Value)
	assert.Zero(t, count)
	assert.False(t, executed)
}
