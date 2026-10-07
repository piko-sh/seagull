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

package panicguard_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/panicguard"
)

const operation = "parsing statement"

var (
	errInterrupted = errors.New("walk budget spent")
	errCall        = errors.New("query failed")
	errDisk        = errors.New("disk on fire")
)

type interruptValue struct {
	err error
}

func (value interruptValue) InterruptError() error {
	return value.err
}

func newCapturingContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	return logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(logs, nil))), logs
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

func recoverInOwnDeferredFunction(ctx context.Context, value any) (err error) {
	defer func() {
		err = panicguard.Recovered(ctx, operation, recover())
	}()
	panic(value)
}

func TestRunTurnsAPanicIntoAnError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		call           func() error
		wantIs         []error
		wantNotIs      []error
		name           string
		wantMessage    string
		wantRecords    int
		wantPanicError bool
	}{
		{
			name:           "panic with a plain value",
			call:           func() error { panic("boom") },
			wantIs:         []error{fault.ErrPanic},
			wantMessage:    "parsing statement: panic: boom",
			wantRecords:    1,
			wantPanicError: true,
		},
		{
			name:           "panic with an error value",
			call:           func() error { panic(fmt.Errorf("writing: %w", errDisk)) },
			wantIs:         []error{fault.ErrPanic, errDisk},
			wantMessage:    "parsing statement: panic: writing: disk on fire",
			wantRecords:    1,
			wantPanicError: true,
		},
		{
			name:        "panic with an interrupt",
			call:        func() error { panic(interruptValue{err: errInterrupted}) },
			wantIs:      []error{errInterrupted},
			wantNotIs:   []error{fault.ErrPanic},
			wantMessage: "walk budget spent",
			wantRecords: 0,
		},
		{
			name:           "panic with an interrupt that carries no error",
			call:           func() error { panic(interruptValue{err: nil}) },
			wantIs:         []error{fault.ErrPanic},
			wantMessage:    "parsing statement: panic: {<nil>}",
			wantRecords:    1,
			wantPanicError: true,
		},
		{
			name:           "panic with nil",
			call:           func() error { panic(nil) },
			wantIs:         []error{fault.ErrPanic},
			wantMessage:    "parsing statement: panic: " + new(runtime.PanicNilError).Error(),
			wantRecords:    1,
			wantPanicError: true,
		},
		{
			name:        "no panic",
			call:        func() error { return nil },
			wantRecords: 0,
		},
		{
			name:        "no panic with an error",
			call:        func() error { return errCall },
			wantIs:      []error{errCall},
			wantNotIs:   []error{fault.ErrPanic},
			wantMessage: "query failed",
			wantRecords: 0,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, logs := newCapturingContext(t)

			err := panicguard.Run(ctx, operation, testCase.call)

			if testCase.wantMessage == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, testCase.wantMessage)
			}
			for _, target := range testCase.wantIs {
				assert.ErrorIs(t, err, target)
			}
			for _, target := range testCase.wantNotIs {
				assert.NotErrorIs(t, err, target)
			}
			_, isPanicError := errors.AsType[*panicguard.Error](err)
			assert.Equal(t, testCase.wantPanicError, isPanicError)
			assert.Len(t, decodeRecords(t, logs), testCase.wantRecords)
		})
	}
}

func TestRunValueReturnsTheZeroValueWhenTheCallPanics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		call        func() (int, error)
		wantIs      []error
		name        string
		wantResult  int
		wantRecords int
	}{
		{name: "no panic", call: func() (int, error) { return 7, nil }, wantResult: 7, wantRecords: 0},
		{name: "no panic with an error", call: func() (int, error) { return 3, errCall }, wantIs: []error{errCall}, wantResult: 3, wantRecords: 0},
		{name: "panic with a plain value", call: func() (int, error) { panic("boom") }, wantIs: []error{fault.ErrPanic}, wantResult: 0, wantRecords: 1},
		{name: "panic with an error value", call: func() (int, error) { panic(errDisk) }, wantIs: []error{fault.ErrPanic, errDisk}, wantResult: 0, wantRecords: 1},
		{
			name:        "panic with an interrupt",
			call:        func() (int, error) { panic(interruptValue{err: errInterrupted}) },
			wantIs:      []error{errInterrupted},
			wantResult:  0,
			wantRecords: 0,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, logs := newCapturingContext(t)

			result, err := panicguard.RunValue(ctx, operation, testCase.call)

			assert.Equal(t, testCase.wantResult, result)
			if len(testCase.wantIs) == 0 {
				require.NoError(t, err)
			}
			for _, target := range testCase.wantIs {
				assert.ErrorIs(t, err, target)
			}
			assert.Len(t, decodeRecords(t, logs), testCase.wantRecords)
		})
	}
}

func TestRecoveredLogsTheStackOnceAndReturnsAStackFreeError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value     any
		name      string
		wantPanic string
	}{
		{name: "plain value", value: "boom", wantPanic: "boom"},
		{name: "error value", value: errDisk, wantPanic: "disk on fire"},
		{name: "integer value", value: 42, wantPanic: "42"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, logs := newCapturingContext(t)

			err := recoverInOwnDeferredFunction(ctx, testCase.value)

			panicError, isPanicError := errors.AsType[*panicguard.Error](err)
			require.True(t, isPanicError)
			assert.Equal(t, &panicguard.Error{Value: testCase.value, Operation: operation}, panicError)
			assert.NotContains(t, err.Error(), "goroutine")
			assert.NotContains(t, err.Error(), "recoverInOwnDeferredFunction")

			records := decodeRecords(t, logs)
			require.Len(t, records, 1)
			record := records[0]
			assert.Equal(t, slog.LevelWarn.String(), record[slog.LevelKey])
			assert.Equal(t, "recovered a panic", record[slog.MessageKey])
			assert.Equal(t, operation, record["operation"])
			assert.Equal(t, testCase.wantPanic, record["panic"])
			stack, ok := record["stack"].(string)
			require.True(t, ok, "stack is a string attribute")
			assert.Contains(t, stack, "goroutine ")
			assert.Contains(t, stack, "panicguard_test.recoverInOwnDeferredFunction")
		})
	}
}

func TestRecoveredHandsBackNilAndInterruptsWithoutLogging(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value   any
		wantErr error
		name    string
	}{
		{name: "nil value", value: nil, wantErr: nil},
		{name: "interrupt", value: interruptValue{err: errInterrupted}, wantErr: errInterrupted},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, logs := newCapturingContext(t)

			err := panicguard.Recovered(ctx, operation, testCase.value)

			assert.Equal(t, testCase.wantErr, err)
			assert.Empty(t, decodeRecords(t, logs))
		})
	}
}
