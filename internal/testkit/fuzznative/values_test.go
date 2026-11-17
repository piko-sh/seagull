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

package fuzznative

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

func TestNativeOracleRejectsCorruptedObservations(t *testing.T) {
	t.Parallel()
	model, ok := fuzzcase.DecodeRuntime("postgres", []byte{12, 0, 3, 1, 0, 2, 1, 0})
	require.True(t, ok, "valid model rejected")
	rows := make([]Row, 0, len(model.Values))
	for index, value := range model.Values {
		rows = append(rows, Row{ID: int64(index + 1), Value: value, Null: value.Null})
	}
	response := Response{Completed: runStageNames(), Plain: rows, Stream: rows, Early: rows[:1], Page: rows[1:2], Reused: rows[1:2], Fresh: rows, Count: 2, AfterCancel: rows, Cancelled: true}
	require.Empty(t, Problems(model, &response))
	faults := map[string]func(*Response){
		"driver mode":             func(r *Response) { r.Native = true },
		"lost cancellation":       func(r *Response) { r.Cancelled = false },
		"cancellation loses data": func(r *Response) { r.AfterCancel = nil },
		"preparation mode":        func(r *Response) { r.Prepared = true },
		"missing stage":           func(r *Response) { r.Completed = r.Completed[:5] },
		"error":                   func(r *Response) { r.Error = "native failure" },
		"panic":                   func(r *Response) { r.Panic = "recovered panic" },
		"lost row":                func(r *Response) { r.Plain = r.Plain[:2] },
		"reordered":               func(r *Response) { r.Stream = []Row{rows[1], rows[0], rows[2]} },
		"ignores early stop":      func(r *Response) { r.Early = rows },
		"lost filter":             func(r *Response) { r.Page = rows[:1] },
		"count applies limit":     func(r *Response) { r.Count = 1 },
		"count mutates builder":   func(r *Response) { r.Reused = rows },
		"builder state leaks":     func(r *Response) { r.Fresh = rows[1:] },
		"empty IN admits rows":    func(r *Response) { r.EmptyCount = 1 },
		"NULL mask":               func(r *Response) { r.Plain = append([]Row(nil), rows...); r.Plain[0].Null = true },
		"value corruption":        func(r *Response) { r.Plain = append([]Row(nil), rows...); r.Plain[0].Value.Number = "999" },
	}
	for name, corrupt := range faults {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bad := response
			corrupt(&bad)
			require.NotEmpty(t, Problems(model, &bad), "fault escaped independent oracle")
		})
	}
}

func TestNativeOracleKeepsModeProblemsWhenTheRunFailed(t *testing.T) {
	t.Parallel()
	model, ok := fuzzcase.DecodeRuntime("postgres", []byte{12, 0, 3, 1, 0, 2, 1, 0})
	require.True(t, ok)
	response := Response{Native: !model.Native, Prepared: model.Prepared, Error: "native failure"}
	require.Len(t, Problems(model, &response), 2)
}

func TestExactComparisonsRetainNumericInformation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		left, right fuzzcase.TypedValue
		equal       bool
	}{
		{name: "unsigned sign bit", left: fuzzcase.TypedNumber(kindUnsigned, "18446744073709551615", 0), right: fuzzcase.TypedNumber(kindUnsigned, "18446744073709551614", 0), equal: false},
		{name: "decimal spelling", left: fuzzcase.TypedNumber(kindDecimal, "100", 2), right: fuzzcase.TypedNumber(kindDecimal, "1", 0), equal: true},
		{name: "decimal rounding", left: fuzzcase.TypedNumber(kindDecimal, "9007199254740993", 0), right: fuzzcase.TypedNumber(kindDecimal, "9007199254740992", 0), equal: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.equal, CompareValue(test.left, test.right) == nil)
		})
	}
	for _, text := range []string{`{"n":9007199254740993}`, `{"n":"9007199254740992"}`} {
		left := fuzzcase.TypedNumber(kindJSON, numberZero, 0)
		left.Text = `{"n":9007199254740992}`
		right := left
		right.Text = text
		require.Error(t, CompareValue(left, right), "JSON lost number digits or kind")
	}
	left := fuzzcase.TypedNumber(kindFloat, numberZero, 0)
	left.Bits = math.Float64bits(math.SmallestNonzeroFloat64)
	right := left
	right.Bits = 0
	require.Error(t, CompareValue(left, right), "subnormal underflow escaped")
	left.Bits = math.Float64bits(math.Copysign(0, -1))
	require.NoError(t, CompareValue(left, right), "documented signed-zero equivalence rejected")
}

func TestArgumentAndObservationPreserveNullAndBounds(t *testing.T) {
	t.Parallel()
	missing := fuzzcase.TypedNumber(kindBinary, numberZero, 0)
	missing.Null = true
	null, err := Argument(reflect.TypeFor[[]byte](), missing)
	require.NoError(t, err)
	require.True(t, null.IsNil(), "NULL binary")
	empty := missing
	empty.Null = false
	empty.Bytes = []byte{}
	present, err := Argument(reflect.TypeFor[[]byte](), empty)
	require.NoError(t, err)
	require.False(t, present.IsNil(), "empty binary became NULL")
	_, err = Observed(present, kindBinary, true)
	require.Error(t, err, "populated SQL NULL accepted")
	maximum := fuzzcase.TypedNumber(kindUnsigned, "18446744073709551615", 0)
	argument, err := Argument(reflect.TypeFor[uint64](), maximum)
	require.NoError(t, err)
	require.Equal(t, uint64(math.MaxUint64), argument.Uint())
	_, err = Argument(reflect.TypeFor[int64](), maximum)
	require.Error(t, err, "unsigned overflow accepted")
	_, err = observedBoolean(reflect.ValueOf(uint8(2)))
	require.Error(t, err, "invalid predicate accepted")
}

func TestNativeArrayReferencePreservesExactIntegers(t *testing.T) {
	t.Parallel()
	value, err := NativeValue(kindArray, `[9223372036854775807,"-9223372036854775808",null]`, false)
	require.NoError(t, err)
	require.Equal(t, "9223372036854775807", value.Elements[0].Number)
	require.Equal(t, "-9223372036854775808", value.Elements[1].Number)
	require.True(t, value.Elements[2].Null)
	_, err = NativeValue(kindArray, `[1.5]`, false)
	require.Error(t, err, "fractional integer accepted")
	_, err = nativeArray(strings.Repeat(" ", 8) + `[true]`)
	require.Error(t, err, "Boolean integer accepted")
}

func TestJSONNumbersKeepHugeExponentsBounded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		left, right string
		equal       bool
	}{
		{left: `1e999999999999999999999999`, right: `10e999999999999999999999998`, equal: true},
		{left: `1e-999999999999999999999999`, right: `0.1e-999999999999999999999998`, equal: true},
		{left: `-0e999999999999999999999999`, right: `0`, equal: true},
		{left: `123.45000e+42`, right: `12345e40`, equal: true},
		{left: `1e999999999999999999999999`, right: `1e999999999999999999999998`, equal: false},
		{left: `9007199254740993`, right: `9007199254740992`, equal: false},
		{left: `-1e20`, right: `1e20`, equal: false},
	}
	for _, test := range cases {
		left, err := exactJSON(test.left)
		require.NoError(t, err)
		right, err := exactJSON(test.right)
		require.NoError(t, err)
		require.Equal(t, test.equal, reflect.DeepEqual(left, right), "%s and %s", test.left, test.right)
	}
	_, err := canonicalJSONNumber("1ex")
	require.Error(t, err)
}

func TestTransactionOracleRejectsMissingLifecycleEffects(t *testing.T) {
	t.Parallel()
	model, ok := fuzzcase.DecodeRuntime("postgres", []byte{12, 0, 2, 0, 16})
	require.True(t, ok)
	require.True(t, model.Transactions)
	baseline := make([]Row, 0, len(model.Values))
	for index, value := range model.Values {
		baseline = append(baseline, Row{ID: int64(index + 1), Value: value, Null: value.Null})
	}
	value := fuzzcase.RuntimeTransactionValue(model)
	final := append(append([]Row(nil), baseline...), Row{ID: transactionIdentifier, Value: value, Null: value.Null})
	response := Response{BeforeRollback: final, AfterRollback: baseline, BeforeCommit: final, AfterCommit: final, AfterCancel: final, ClosedTransactions: closedTransactionChecks, Cancelled: true}
	require.Empty(t, lifecycleProblems(model, &response, baseline))
	faults := map[string]func(*Response){
		"insert lost before rollback":        func(r *Response) { r.BeforeRollback = baseline },
		"rollback committed":                 func(r *Response) { r.AfterRollback = final },
		"insert lost before commit":          func(r *Response) { r.BeforeCommit = baseline },
		"commit rolled back":                 func(r *Response) { r.AfterCommit = baseline },
		"completed transaction reused":       func(r *Response) { r.ClosedTransactions = 1 },
		"cancelled read accepted":            func(r *Response) { r.Cancelled = false },
		"connection lost after cancellation": func(r *Response) { r.AfterCancel = nil },
	}
	for name, corrupt := range faults {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bad := response
			corrupt(&bad)
			require.NotEmpty(t, lifecycleProblems(model, &bad, baseline), "lifecycle fault escaped")
		})
	}
}
