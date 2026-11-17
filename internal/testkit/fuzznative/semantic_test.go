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
	"database/sql"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
	"seagull.sh/seagull/internal/testkit/fuzzsemantic"
)

func TestSemanticCellsAgreeAcrossGoTypes(t *testing.T) {
	t.Parallel()
	var missing *int64
	for _, test := range []struct {
		value any
		want  fuzzsemantic.Cell
	}{
		{value: int32(3), want: fuzzsemantic.Cell{Text: "3"}},
		{value: uint64(3), want: fuzzsemantic.Cell{Text: "3"}},
		{value: 3.0, want: fuzzsemantic.Cell{Text: "3"}},
		{value: "3.50", want: fuzzsemantic.Cell{Text: "7/2"}},
		{value: " 3", want: fuzzsemantic.Cell{Text: " 3"}},
		{value: true, want: fuzzsemantic.Cell{Text: "true"}},
		{value: []byte("ab"), want: fuzzsemantic.Cell{Text: "ab"}},
		{value: missing, want: fuzzsemantic.Cell{Null: true}},
		{value: Null[int64]{}, want: fuzzsemantic.Cell{Null: true}},
		{value: sql.NullInt64{Int64: 4, Valid: true}, want: fuzzsemantic.Cell{Text: "4"}},
		{value: sql.NullInt64{}, want: fuzzsemantic.Cell{Null: true}},
	} {
		cell, err := semanticCell(reflect.ValueOf(test.value))
		require.NoError(t, err, "%#v", test.value)
		assert.Equal(t, test.want, cell, "%#v", test.value)
	}
	_, err := semanticCell(reflect.ValueOf(math.Inf(1)))
	require.Error(t, err)
	_, err = semanticCell(reflect.ValueOf(map[int]int{1: 1}))
	require.Error(t, err)
}

func TestSemanticRowsReadStructsAndBareValues(t *testing.T) {
	t.Parallel()
	type row struct {
		ID    int64
		Label *string
	}
	cells, err := semanticRow(reflect.ValueOf(row{ID: 1}))
	require.NoError(t, err)
	require.Equal(t, []fuzzsemantic.Cell{{Text: "1"}, {Null: true}}, cells)
	cells, err = semanticRow(reflect.ValueOf(int64(5)))
	require.NoError(t, err)
	require.Equal(t, []fuzzsemantic.Cell{{Text: "5"}}, cells)
}

func TestSemanticArgumentsFitGeneratedParameters(t *testing.T) {
	t.Parallel()
	type parameters struct {
		A Null[int32]
		B *uint8
		C string
		D any
		E float64
		F sql.NullInt64
	}
	signature := reflect.TypeOf(func(context.Context, parameters) error { return nil })
	arguments, err := semanticArguments(signature, []int64{1, 2, 3, 4, 5, 6})
	require.NoError(t, err)
	filled, ok := reflect.TypeAssert[parameters](arguments[0])
	require.True(t, ok)
	require.Equal(t, int32(1), filled.A.V)
	require.Equal(t, uint8(2), *filled.B)
	require.Equal(t, "3", filled.C)
	require.Equal(t, int64(4), filled.D)
	require.InDelta(t, 5.0, filled.E, 0)
	require.Equal(t, sql.NullInt64{Int64: 6, Valid: true}, filled.F)

	_, err = semanticArguments(signature, []int64{1})
	require.Error(t, err)
	_, err = semanticArgument(reflect.TypeFor[int8](), 300)
	require.Error(t, err)
	_, err = semanticArgument(reflect.TypeFor[uint8](), -1)
	require.Error(t, err)
	_, err = semanticArgument(reflect.TypeFor[[]int](), 1)
	require.Error(t, err)
	single := reflect.TypeOf(func(context.Context, int64) error { return nil })
	arguments, err = semanticArguments(single, []int64{9})
	require.NoError(t, err)
	require.Equal(t, int64(9), arguments[0].Int())
	none := reflect.TypeOf(func(context.Context) error { return nil })
	arguments, err = semanticArguments(none, nil)
	require.NoError(t, err)
	require.Empty(t, arguments)
}

func TestErrorClassesSeparateBuilderRefusals(t *testing.T) {
	t.Parallel()
	refusal := errors.New("sgl: unknown operator in runtime query filter: \"==\"")
	assert.Equal(t, fuzzbuilder.ClassUnknownOperator, classifyBuilderError(refusal))
	assert.Equal(t, fuzzsemantic.ClassBuilder, semanticClass(refusal))
	assert.Equal(t, fuzzbuilder.ClassNoRows, classifyBuilderError(sql.ErrNoRows))
	assert.Equal(t, fuzzbuilder.ClassError, classifyBuilderError(errors.New("boom")))
	assert.Equal(t, fuzzsemantic.ClassNative, semanticClass(errors.New("boom")))
}

func TestBuilderCellsReadEachFieldKind(t *testing.T) {
	t.Parallel()
	type row struct {
		ID    uint32
		Label string
		Score *int64
	}
	cells, err := builderCells(reflect.ValueOf(row{ID: 2, Label: "a"}))
	require.NoError(t, err)
	require.Equal(t, []fuzzbuilder.Cell{{Integer: 2}, {Text: "a", IsText: true}, {Null: true}}, cells)
	_, err = builderCells(reflect.ValueOf(struct{ Wide uint64 }{Wide: math.MaxUint64}))
	require.Error(t, err)
	_, err = builderCells(reflect.ValueOf(3))
	require.Error(t, err)
}
