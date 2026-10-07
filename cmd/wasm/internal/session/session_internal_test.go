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

package session

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

type logRecord struct {
	Level     string `json:"level"`
	Message   string `json:"msg"`
	Operation string `json:"operation"`
	Panic     string `json:"panic"`
	Stack     string `json:"stack"`
}

func logRecords(t *testing.T, logged *bytes.Buffer) []logRecord {
	t.Helper()
	var records []logRecord
	for line := range strings.Lines(logged.String()) {
		var record logRecord
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		records = append(records, record)
	}
	return records
}

func TestGuardedLogsAPanicsStackOnceAndAnswersWithoutIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		work    func() Response
		message string
	}{
		{
			name:    "a panic with a value",
			work:    func() Response { panic("injected failure") },
			message: "internal error: injected failure",
		},
		{
			name: "a panic while building the response",
			work: func() Response {
				var resolved *project
				return respond(nil, resolved, &passes{result: nil, report: nil, catalogue: nil}, nil)
			},
			message: "internal error: runtime error: invalid memory address or nil pointer dereference",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var logged bytes.Buffer
			ctx := seagull.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logged, nil)))

			response := guarded(ctx, newSteppingClock(time.Millisecond), testCase.work)

			require.False(t, response.OK)
			require.True(t, response.Internal)
			require.Equal(t, testCase.message, response.Error)
			require.NotContains(t, response.Error, "goroutine")
			require.Equal(t, ProtocolVersion, response.V)
			require.InDelta(t, 1.0, response.DurationMS, 0)
			records := logRecords(t, &logged)
			require.Len(t, records, 1)
			require.Equal(t, "WARN", records[0].Level)
			require.Equal(t, runOperation, records[0].Operation)
			require.Contains(t, records[0].Stack, "goroutine")
			require.Contains(t, records[0].Stack, "session.guarded")
		})
	}
}

func TestRunTimesTheRunWithItsClock(t *testing.T) {
	t.Parallel()

	files, err := Starter(DefaultDialect)
	require.NoError(t, err)
	clock := newSteppingClock(time.Millisecond)

	response := runWithClock(t.Context(), Request{Files: files, Override: Override{}, V: ProtocolVersion}, clock)

	require.True(t, response.OK, response.Error)
	require.InDelta(t, float64(clock.readCount()-1), response.DurationMS, 0)
	require.NotEmpty(t, response.Stages)
	for _, stage := range response.Stages {
		require.Positive(t, stage.MS, stage.Stage)
		require.InDelta(t, float64(int(stage.MS)), stage.MS, 0, stage.Stage)
		require.Less(t, stage.MS, response.DurationMS, stage.Stage)
	}
}
