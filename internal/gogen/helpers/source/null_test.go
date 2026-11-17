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

package source

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNullScansAValueAndNull(t *testing.T) {
	t.Parallel()

	var text Null[string]
	require.NoError(t, text.Scan([]byte("harbour")))
	require.Equal(t, Null[string]{V: "harbour", Valid: true}, text)
	require.NoError(t, text.Scan(nil))
	require.Equal(t, Null[string]{}, text, "NULL clears the value")

	var number Null[int32]
	require.NoError(t, number.Scan(int64(42)))
	require.Equal(t, Null[int32]{V: 42, Valid: true}, number)
	require.Error(t, number.Scan(int64(1)<<40), "a value that overflows T is refused")

	type mood string
	var named Null[mood]
	require.NoError(t, named.Scan("calm"))
	require.Equal(t, Null[mood]{V: "calm", Valid: true}, named, "a named string type reads its text")
}

func TestNullWritesNullOrItsValue(t *testing.T) {
	t.Parallel()

	value, err := Null[int32]{}.Value()
	require.NoError(t, err)
	require.Nil(t, value)

	value, err = Null[int32]{V: 7, Valid: true}.Value()
	require.NoError(t, err)
	require.Equal(t, int64(7), value, "database/sql's driver values hold integers as int64")
}

func TestNullEncodesToJSONAsAPointerDoes(t *testing.T) {
	t.Parallel()

	type row struct {
		Note    Null[string]    `json:"note"`
		Omitted Null[string]    `json:"omitted,omitzero"`
		At      Null[time.Time] `json:"at"`
	}
	at := time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC)
	encoded, err := json.Marshal(row{Note: Null[string]{V: "x", Valid: true}, At: Null[time.Time]{V: at, Valid: true}})
	require.NoError(t, err)
	require.JSONEq(t, `{"note":"x","at":"2026-11-16T09:30:00Z"}`, string(encoded))

	encoded, err = json.Marshal(row{})
	require.NoError(t, err)
	require.JSONEq(t, `{"note":null,"at":null}`, string(encoded), "omitzero leaves a NULL value out")

	var decoded row
	require.NoError(t, json.Unmarshal([]byte(`{"note":"y","at":null}`), &decoded))
	require.Equal(t, Null[string]{V: "y", Valid: true}, decoded.Note)
	require.False(t, decoded.At.Valid)
	require.Error(t, json.Unmarshal([]byte(`{"note":3}`), &decoded))
}

func TestNullGetPtrAndIsZero(t *testing.T) {
	t.Parallel()

	set := Null[int64]{V: 9, Valid: true}
	value, ok := set.Get()
	require.Equal(t, int64(9), value)
	require.True(t, ok)
	pointer := set.Ptr()
	require.Equal(t, int64(9), *pointer)
	*pointer = 10
	require.Equal(t, int64(9), set.V, "Ptr points to a copy")
	require.False(t, set.IsZero())

	var unset Null[int64]
	require.Nil(t, unset.Ptr())
	require.True(t, unset.IsZero())
}

func TestNullScanAllocatesNothingBeyondTheValue(t *testing.T) {
	var target Null[int64]
	var source any = int64(123456)
	allocations := testing.AllocsPerRun(100, func() {
		if err := target.Scan(source); err != nil {
			t.Fatal(err)
		}
	})
	require.Zero(t, allocations, "reading an integer into a Null allocates nothing")
}

func TestNullScansNullWithoutAllocating(t *testing.T) {
	target := NullOf("held")
	allocations := testing.AllocsPerRun(100, func() {
		if err := target.Scan(nil); err != nil {
			t.Fatal(err)
		}
	})
	require.Zero(t, allocations, "reading NULL into a Null allocates nothing")
	require.Equal(t, Null[string]{}, target)
}

func TestNullOfAndNullFromBuildANull(t *testing.T) {
	t.Parallel()

	require.Equal(t, Null[string]{V: "gull", Valid: true}, NullOf("gull"))
	value := 3
	require.Equal(t, Null[int]{V: 3, Valid: true}, NullFrom(&value))
	require.Equal(t, Null[int]{}, NullFrom[int](nil))
}

func TestNullScansAValueOfItsOwnTypeOrAPointerToOne(t *testing.T) {
	t.Parallel()

	var huge Null[big.Int]
	require.NoError(t, huge.Scan(big.NewInt(1234)), "a driver may supply a pointer to the value's own type")
	require.True(t, huge.Valid)
	require.Equal(t, "1234", huge.V.String())
	require.NoError(t, huge.Scan((*big.Int)(nil)))
	require.False(t, huge.Valid)

	var at Null[time.Time]
	moment := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, at.Scan(moment))
	require.Equal(t, Null[time.Time]{V: moment, Valid: true}, at)
}

type pointerMarshaler struct{ text string }

func (p *pointerMarshaler) MarshalText() ([]byte, error) { return []byte("p:" + p.text), nil }

func TestNullEncodesThroughAPointerReceiversMarshaler(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(NullOf(pointerMarshaler{text: "x"}))
	require.NoError(t, err)
	require.JSONEq(t, `"p:x"`, string(encoded))
}

func TestNullHandsAWideUnsignedValueOverAsItIs(t *testing.T) {
	t.Parallel()

	value, err := NullOf(uint64(1) << 63).Value()
	require.NoError(t, err)
	require.Equal(t, uint64(1)<<63, value, "the driver binds a uint64 with the high bit set itself")

	value, err = NullOf(uint64(7)).Value()
	require.NoError(t, err)
	require.Equal(t, int64(7), value)
}

func TestNullReadsANarrowNumberWithoutAllocating(t *testing.T) {
	var small Null[int16]
	require.NoError(t, small.Scan(int64(-12)))
	require.Equal(t, NullOf(int16(-12)), small)
	require.Error(t, small.Scan(int64(1)<<20), "an integer that does not fit is refused")

	var single Null[float32]
	require.NoError(t, single.Scan(float64(float32(2.5))))
	require.Equal(t, NullOf(float32(2.5)), single)

	var target Null[int32]
	var source any = int64(99)
	allocations := testing.AllocsPerRun(100, func() {
		if err := target.Scan(source); err != nil {
			t.Fatal(err)
		}
	})
	require.Zero(t, allocations)
}
