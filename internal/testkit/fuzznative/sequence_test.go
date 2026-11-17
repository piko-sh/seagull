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
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
	"seagull.sh/seagull/internal/testkit/fuzztyped"
)

type Null[T any] struct {
	V     T
	Valid bool
}

type builderRow struct {
	ID    int64
	Label Null[string]
}

type builderStub struct {
	failure error
	rows    []builderRow
}

func (b *builderStub) All(context.Context) ([]builderRow, error) {
	return b.rows, b.failure
}

func TestTypedValuesRoundTripThroughGoTypes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		target reflect.Type
		value  fuzztyped.Value
		shape  string
	}{
		{target: reflect.TypeFor[Null[float64]](), value: fuzztyped.Float(math.Copysign(0, -1)), shape: fuzztyped.KindFloat},
		{target: reflect.TypeFor[Null[float64]](), value: fuzztyped.Float(math.Inf(-1)), shape: fuzztyped.KindFloat},
		{target: reflect.TypeFor[Null[float64]](), value: fuzztyped.Null(), shape: fuzztyped.KindFloat},
		{target: reflect.TypeFor[*string](), value: fuzztyped.Text("a\x00b"), shape: fuzztyped.KindText},
		{target: reflect.TypeFor[Null[time.Time]](), value: fuzztyped.Scalar(fuzztyped.KindTime, "1969-12-31T23:59:59.999999"), shape: fuzztyped.KindTime},
		{target: reflect.TypeFor[[]*int64](), value: fuzztyped.List(fuzztyped.Integer(1), fuzztyped.Null()), shape: fuzztyped.KindList + " " + fuzztyped.KindInteger},
		{target: reflect.TypeFor[[][]int64](), value: fuzztyped.List(fuzztyped.List(), fuzztyped.List(fuzztyped.Integer(math.MinInt64))),
			shape: "list list " + fuzztyped.KindInteger},
		{target: reflect.TypeFor[[]*int64](), value: fuzztyped.List(), shape: fuzztyped.KindList + " " + fuzztyped.KindInteger},
	} {
		argument, err := typedArgument(test.target, test.value)
		require.NoError(t, err, "%v into %s", test.value, test.target)
		observed, err := typedObserve(argument, test.shape)
		assert.NoError(t, err)
		assert.True(t, observed.Equal(test.value), "%v through %s came back as %v", test.value, test.target, observed)
	}
}

func TestTypedInstantKeepsItsOffsetAsArgument(t *testing.T) {
	t.Parallel()
	argument, err := typedArgument(reflect.TypeFor[time.Time](), fuzztyped.Scalar(fuzztyped.KindInstant, "2024-01-01T00:30:00+14:00"))
	require.NoError(t, err)
	moment, ok := reflect.TypeAssert[time.Time](argument)
	require.True(t, ok)
	_, offset := moment.Zone()
	assert.Equal(t, 14*60*60, offset)
}

func TestTypedArgumentRefusesNullForValues(t *testing.T) {
	t.Parallel()
	_, err := typedArgument(reflect.TypeFor[int64](), fuzztyped.Null())
	assert.Error(t, err, "NULL entered a non-nullable parameter")
}

func TestTypedArgumentsRefuseShortNames(t *testing.T) {
	t.Parallel()
	type parameters struct{ A, B int64 }
	signature := reflect.TypeOf(func(context.Context, parameters) error { return nil })
	current := fuzztyped.Call{Arguments: []fuzztyped.Value{fuzztyped.Integer(1), fuzztyped.Integer(2)}, Names: []string{"A"}}
	_, err := typedArguments(signature, current)
	require.Error(t, err)
}

func TestBuilderTerminalObservesRowsAndClassifiesErrors(t *testing.T) {
	t.Parallel()
	stub := &builderStub{failure: nil, rows: []builderRow{{ID: 1, Label: Null[string]{V: "a", Valid: true}}, {ID: 2, Label: Null[string]{V: "", Valid: false}}}}
	observation, err := terminal(t.Context(), reflect.ValueOf(stub), fuzzbuilder.OperationAll)
	require.NoError(t, err)
	require.Len(t, observation.Rows, 2)
	require.True(t, observation.Rows[1][1].Null)
	require.Equal(t, "a", observation.Rows[0][1].Text)
	stub.failure = errors.New("sgl: unknown column in runtime query filter")
	observation, err = terminal(t.Context(), reflect.ValueOf(stub), fuzzbuilder.OperationAll)
	require.NoError(t, err)
	assert.Equal(t, fuzzbuilder.ClassUnknownColumn, observation.Class)
	observation, err = terminal(t.Context(), reflect.ValueOf(stub), fuzzbuilder.OperationCount)
	require.NoError(t, err)
	assert.Equal(t, fuzzbuilder.ClassMissingMethod, observation.Class)
}

func TestBuilderStepRefusesASlotOutsideTheModel(t *testing.T) {
	t.Parallel()
	run := builderRun{queries: reflect.Value{}, slots: [2]reflect.Value{}}
	_, err := run.step(t.Context(), fuzzbuilder.Step{Slot: 2})
	require.Error(t, err)
}

func TestConsumeIteratorReportsMisbehavingIterators(t *testing.T) {
	t.Parallel()
	keepGoing := func(yield func(int, error) bool) {
		yield(1, nil)
		yield(2, nil)
	}
	visits := 0
	err := consumeIterator(reflect.ValueOf(keepGoing), func([]reflect.Value) (bool, error) {
		visits++
		return false, nil
	})
	require.ErrorIs(t, err, errYieldAfterStop)
	require.Equal(t, 1, visits)

	endless := func(yield func(int, error) bool) {
		for index := 0; yield(index, nil); index++ {
		}
	}
	err = consumeIterator(reflect.ValueOf(endless), func([]reflect.Value) (bool, error) { return true, nil })
	require.ErrorIs(t, err, errStreamBound)

	require.Error(t, consumeIterator(reflect.ValueOf(func(int) {}), nil))
}
