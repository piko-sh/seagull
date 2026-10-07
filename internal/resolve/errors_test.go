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

package resolve

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
)

func TestErrorCarriesItsCodeOutsideTheMessage(t *testing.T) {
	t.Parallel()

	resolveError := newError(diag.CodeUnknownFunction, "unknown function %q", "nonexistent")

	require.Equal(t, `unknown function "nonexistent"`, resolveError.Error())
	require.Equal(t, diag.CodeUnknownFunction, ExtractErrorCode(resolveError))
	require.Equal(t, diag.SeverityWarning, resolveError.Severity)
}

func TestExtractErrorCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
		want string
	}{
		{name: "typed error", err: newError(diag.CodeUnknownColumn, "unknown column %q", "x"), want: diag.CodeUnknownColumn},
		{name: "wrapped typed error", err: fmt.Errorf("context: %w", newError(diag.CodeAmbiguousColumn, "ambiguous")), want: diag.CodeAmbiguousColumn},
		{name: "a code in the message is not parsed", err: errors.New("Q001: unknown column"), want: diag.CodeExpressionTypeError},
		{name: "untyped error", err: errors.New("something went wrong"), want: diag.CodeExpressionTypeError},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, ExtractErrorCode(testCase.err))
		})
	}
}

func TestSpanKnown(t *testing.T) {
	t.Parallel()

	require.False(t, Span{Start: 0, End: 0}.Known())
	require.True(t, Span{Start: 3, End: 7}.Known())
}

func TestEngineFailureSortsWhatTheDialectReturned(t *testing.T) {
	t.Parallel()

	cause := errors.New("the run was abandoned")
	testCases := []struct {
		failure         error
		wantInterrupted []error
		name            string
		wantProblem     string
		cancelWith      error
		cancelled       bool
	}{
		{name: "no failure", failure: nil},
		{name: "a recovered panic is a problem", failure: fmt.Errorf("resolve: %w: boom", fault.ErrPanic), wantProblem: diag.CodeRecoveredPanic},
		{name: "a cancellation is an interruption", failure: context.Canceled, wantInterrupted: []error{context.Canceled}},
		{name: "a wrapped deadline is an interruption", failure: fmt.Errorf("walk: %w", context.DeadlineExceeded), wantInterrupted: []error{context.DeadlineExceeded}},
		{
			name:            "a recovered panic once the context is cancelled is an interruption",
			failure:         fmt.Errorf("resolve: %w: boom", fault.ErrPanic),
			cancelled:       true,
			wantInterrupted: []error{context.Canceled},
		},
		{
			name:            "any failure once the context is cancelled carries the cancellation's cause",
			failure:         errors.New("not resolvable"),
			cancelled:       true,
			cancelWith:      cause,
			wantInterrupted: []error{context.Canceled, cause},
		},
		{name: "any other failure is no opinion", failure: errors.New("not resolvable")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			if testCase.cancelled {
				cancel(testCase.cancelWith)
			}

			problem, interruption := engineFailure(ctx, testCase.failure, `function "probe"`)

			for _, want := range testCase.wantInterrupted {
				require.ErrorIs(t, interruption, want)
			}
			if testCase.wantInterrupted == nil {
				require.NoError(t, interruption)
			}
			if testCase.wantProblem == "" {
				require.Nil(t, problem)
				return
			}
			require.NotNil(t, problem)
			require.Equal(t, testCase.wantProblem, problem.Code)
			require.Equal(t, diag.SeverityError, problem.Severity)
			require.Contains(t, problem.Message, `function "probe"`)
		})
	}
}

func TestAnInterruptionIsNeverPlacedAsAProblem(t *testing.T) {
	t.Parallel()

	interruption := fmt.Errorf("the dialect stopped: %w", context.Canceled)

	require.Same(t, interruption, locatedAt(interruption, ast.Span{Start: 1, End: 4}))
}
