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

package dialectkit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
)

var (
	errProbe       = errors.New("probe failure")
	errTypeTooDeep = dialectkit.LimitSentinel("type name nests too deeply")
)

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

func TestLimitSentinelsWrapTheLimitSentinel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		err      error
		sentinel error
		message  string
	}{
		{
			name:     "parse depth",
			err:      dialectkit.DepthError(256),
			sentinel: dialectkit.ErrParseDepth,
			message:  "statement nests deeper than the parse depth limit (limit 256)",
		},
		{
			name:     "token budget",
			err:      dialectkit.CheckTokenBudget(11, 10),
			sentinel: dialectkit.ErrTokenBudget,
			message:  "statement exceeds the per-statement token budget: 11 tokens, limit 10",
		},
		{
			name:     "token budget stopped at the limit",
			err:      dialectkit.TokenBudgetExceeded(10),
			sentinel: dialectkit.ErrTokenBudget,
			message:  "statement exceeds the per-statement token budget: more than 10 tokens",
		},
		{
			name:     "walk budget",
			err:      dialectkit.ErrWalkBudget,
			sentinel: dialectkit.ErrWalkBudget,
			message:  "the parser stopped advancing through the statement",
		},
		{
			name:     "a dialect's own limit",
			err:      errTypeTooDeep,
			sentinel: errTypeTooDeep,
			message:  "type name nests too deeply",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, testCase.err, testCase.sentinel)
			require.ErrorIs(t, testCase.err, fault.ErrLimit)
			require.NotErrorIs(t, testCase.err, fault.ErrParse)
			require.EqualError(t, testCase.err, testCase.message)
		})
	}
}

func TestCheckTokenBudgetWithinBudget(t *testing.T) {
	t.Parallel()

	require.NoError(t, dialectkit.CheckTokenBudget(10, 10))
	require.NoError(t, dialectkit.CheckTokenBudget(0, 10))
}

func TestLimitSentinelGivesADistinctSentinelEachCall(t *testing.T) {
	t.Parallel()

	first := dialectkit.LimitSentinel("same text")
	second := dialectkit.LimitSentinel("same text")

	require.NotErrorIs(t, first, second)
	require.ErrorIs(t, first, fault.ErrLimit)
	require.ErrorIs(t, second, fault.ErrLimit)
}

func panicWith(ctx context.Context, value any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = dialectkit.RecoveredError(ctx, "probe: analyse query", recovered)
		}
	}()
	panic(value)
}

func TestRecoveredErrorReturnsThePanicAsAnErrorAndLogsItsStackOnce(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value      any
		name       string
		message    string
		wantUnwrap []error
	}{
		{name: "string value", value: "index out of range", message: "probe: analyse query: panic: index out of range",
			wantUnwrap: []error{fault.ErrPanic}},
		{name: "error value", value: errProbe, message: "probe: analyse query: panic: probe failure",
			wantUnwrap: []error{fault.ErrPanic, errProbe}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newCapturingContext(t)
			err := panicWith(ctx, testCase.value)

			require.EqualError(t, err, testCase.message)
			panicError, ok := errors.AsType[*dialectkit.PanicError](err)
			require.True(t, ok)
			require.Equal(t, testCase.value, panicError.Value)
			require.Equal(t, testCase.wantUnwrap, panicError.Unwrap())
			for _, target := range testCase.wantUnwrap {
				require.ErrorIs(t, err, target)
			}
			records := decodeRecords(t, logs)
			require.Len(t, records, 1)
			assert.Equal(t, "WARN", records[0]["level"])
			assert.Equal(t, "probe: analyse query", records[0]["operation"])
			assert.Contains(t, records[0]["stack"], "panicWith")
			assert.NotContains(t, err.Error(), "goroutine")
		})
	}
}

func analyseWithRecover(ctx context.Context, value any) (analysis *strings.Builder, err error) {
	defer func() { dialectkit.Recover(ctx, "probe: analyse query", recover(), &analysis, &err) }()
	analysis = &strings.Builder{}
	if value != nil {
		panic(value)
	}
	return analysis, nil
}

func applyWithRecover(ctx context.Context, value any) (err error) {
	defer func() { dialectkit.Recover[any](ctx, "probe: apply DDL", recover(), nil, &err) }()
	panic(value)
}

func TestRecoverDeferredAtAnEntryPointReturnsThePanicAsAnError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value        any
		wantError    error
		name         string
		wantRecords  int
		wantAnalysis bool
	}{
		{name: "no panic leaves the results", value: nil, wantError: nil, wantRecords: 0, wantAnalysis: true},
		{name: "a fault is logged and zeroes the result", value: "boom", wantError: fault.ErrPanic, wantRecords: 1, wantAnalysis: false},
		{name: "an interrupted walk returns its error unlogged", value: interruptWith(context.Canceled),
			wantError: context.Canceled, wantRecords: 0, wantAnalysis: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newCapturingContext(t)
			analysis, err := analyseWithRecover(ctx, testCase.value)

			if testCase.wantError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, testCase.wantError)
			}
			assert.Equal(t, testCase.wantAnalysis, analysis != nil)
			assert.Len(t, decodeRecords(t, logs), testCase.wantRecords)
		})
	}
}

func TestRecoverWithoutAResultSetsTheError(t *testing.T) {
	t.Parallel()

	ctx, logs := newCapturingContext(t)
	err := applyWithRecover(ctx, "boom")

	require.EqualError(t, err, "probe: apply DDL: panic: boom")
	require.ErrorIs(t, err, fault.ErrPanic)
	require.Len(t, decodeRecords(t, logs), 1)
}

func TestRecoverWithNowhereToReportLetsThePanicGoOn(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "boom", func() {
		defer func() { dialectkit.Recover[any](t.Context(), "probe", recover(), nil, nil) }()
		panic("boom")
	})
}

func TestIsInterruptTellsAnInterruptedWalkFromAFault(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value any
		name  string
		want  bool
	}{
		{name: "an interrupted walk", value: interruptWith(dialectkit.ErrWalkBudget), want: true},
		{name: "a string", value: "boom", want: false},
		{name: "an error", value: errProbe, want: false},
		{name: "nothing", value: nil, want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, dialectkit.IsInterrupt(testCase.value))
		})
	}
}

func interruptWith(err error) (interrupt any) {
	defer func() {
		interrupt = recover()
	}()
	cursor := dialectkit.NewCursor([]dialectkit.Token[int]{{Kind: 1}}, 1, 0)
	cursor.Interrupt = func() error { return err }
	cursor.Current()
	return nil
}

func TestReportEndAtMovesAnEndOfInputErrorToTheNextToken(t *testing.T) {
	t.Parallel()

	next := dialectkit.Token[int]{Kind: 1, Value: "FROM", Raw: "FROM", Start: 40, End: 44}
	end := dialectkit.Token[int]{Kind: 0, Value: "", Raw: "", Start: 37, End: 37}
	atToken := dialectkit.NewParseError(dialectkit.Token[int]{Kind: 1, Value: "x", Raw: "x", Start: 10, End: 11}, "unexpected")
	other := errors.New("not a parse error")

	testCases := []struct {
		err  error
		want error
		next dialectkit.Token[int]
		name string
	}{
		{name: "an end-of-input error moves to the next token", err: dialectkit.NewParseError(end, "expected an expression"),
			next: next, want: &dialectkit.ParseError{Token: "FROM", Message: "expected an expression", Position: 40}},
		{name: "an error at a token stays", err: atToken, next: next, want: atToken},
		{name: "an error at the end stays when nothing follows", err: dialectkit.NewParseError(end, "expected an expression"),
			next: end, want: &dialectkit.ParseError{Token: "", Message: "expected an expression", Position: 37}},
		{name: "another error stays", err: other, next: next, want: other},
		{name: "no error stays none", err: nil, next: next, want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, dialectkit.ReportEndAt(testCase.err, testCase.next))
		})
	}
}

func TestParseErrorMessageGivesNoPosition(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		err     *dialectkit.ParseError
		message string
	}{
		{name: "a token", err: dialectkit.NewParseError(dialectkit.Token[int]{Kind: 1, Value: "x", Raw: "x", Start: 10, End: 11}, "unexpected"),
			message: `syntax error near "x": unexpected`},
		{name: "the same token further on", err: dialectkit.NewParseError(dialectkit.Token[int]{Kind: 1, Value: "x", Raw: "x", Start: 14, End: 15}, "unexpected"),
			message: `syntax error near "x": unexpected`},
		{name: "the end of input", err: dialectkit.NewParseError(dialectkit.Token[int]{Kind: 0, Value: "", Raw: "", Start: 37, End: 37}, "expected FROM"),
			message: "syntax error at end of input: expected FROM"},
		{name: "a token of exactly the quoted length", err: dialectkit.NewParseError(literalToken(strings.Repeat("a", 32)), "unexpected"),
			message: `syntax error near "` + strings.Repeat("a", 32) + `": unexpected`},
		{name: "a huge literal is cut", err: dialectkit.NewParseError(literalToken(strings.Repeat("a", 100_000)), "unexpected"),
			message: `syntax error near "` + strings.Repeat("a", 32) + `...": unexpected`},
		{name: "a literal is cut on a rune boundary", err: dialectkit.NewParseError(literalToken(strings.Repeat("€", 40)), "unexpected"),
			message: `syntax error near "` + strings.Repeat("€", 32) + `...": unexpected`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.EqualError(t, testCase.err, testCase.message)
			require.ErrorIs(t, testCase.err, fault.ErrParse)
		})
	}
}

func TestParseErrorLocationKeepsTheWholeToken(t *testing.T) {
	t.Parallel()

	huge := strings.Repeat("x", 1_000)
	err := dialectkit.NewParseError(dialectkit.Token[int]{Kind: 1, Value: huge, Raw: huge, Start: 7, End: 1_007}, "unexpected")

	token, position := err.Location()

	require.Equal(t, huge, token)
	require.Equal(t, 7, position)
}

func literalToken(text string) dialectkit.Token[int] {
	return dialectkit.Token[int]{Kind: 1, Value: text, Raw: text, Start: 0, End: len(text)}
}

func TestParseErrorAtNamesTheTextAtAPlace(t *testing.T) {
	t.Parallel()

	source := "SELECT 'open € x"
	testCases := []struct {
		want  *dialectkit.ParseError
		name  string
		start int
		end   int
	}{
		{name: "a character", start: 13, end: 16, want: &dialectkit.ParseError{Token: "€", Message: "unexpected character", Position: 13}},
		{name: "a delimiter", start: 7, end: 8, want: &dialectkit.ParseError{Token: "'", Message: "unexpected character", Position: 7}},
		{name: "a span past the end", start: 17, end: 20, want: &dialectkit.ParseError{Token: "x", Message: "unexpected character", Position: 17}},
		{name: "the end of the source", start: 18, end: 19, want: &dialectkit.ParseError{Token: "", Message: "unexpected character", Position: 18}},
		{name: "an empty span", start: 7, end: 7, want: &dialectkit.ParseError{Token: "'", Message: "unexpected character", Position: 7}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, dialectkit.ParseErrorAt(source, testCase.start, testCase.end, "unexpected character"))
		})
	}
}

func TestUnboundParameterErrorNamesTheStatementAndGivesTheHint(t *testing.T) {
	t.Parallel()

	parameter := dialectkit.Token[int]{Kind: 1, Raw: "$1", Value: "$1", Start: 24, End: 26, Quoted: false}
	testCases := []struct {
		name    string
		verb    string
		hint    string
		message string
	}{
		{name: "without a hint", verb: "EXPLAIN", hint: "",
			message: "no parameter can be bound in EXPLAIN statements, which run without analysis"},
		{name: "with a hint", verb: "NOTIFY", hint: "write SELECT pg_notify('channel', $1) instead",
			message: "no parameter can be bound in NOTIFY statements, which run without analysis; " +
				"write SELECT pg_notify('channel', $1) instead"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := dialectkit.UnboundParameterError(parameter, testCase.verb, testCase.hint)

			require.Equal(t, testCase.message, err.Message)
			require.Equal(t, "$1", err.Token)
			require.Equal(t, 24, err.Position)
		})
	}
}

func TestParseErrorAtEndOfInput(t *testing.T) {
	t.Parallel()

	err := dialectkit.NewParseError(endOfInput(12), "expected keyword FROM")

	require.EqualError(t, err, "syntax error at end of input: expected keyword FROM")
	require.ErrorIs(t, err, fault.ErrParse)
	require.Equal(t, 12, err.Position)
}
