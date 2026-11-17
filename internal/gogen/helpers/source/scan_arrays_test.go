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
	"database/sql"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var _ sql.Scanner = sglArrayScanner[string]{}

func TestScanArrayReadsTheTextFormPostgreSQLWrites(t *testing.T) {
	t.Parallel()

	text := `{a,"","NULL",NULL,"with space","q\"uote","back\\slash","{brace}","com,ma"," lead",ünï,"tab` + "\t" + `x",null}`
	for _, source := range []any{text, []byte(text)} {
		var values []*string
		require.NoError(t, sglScanArray(&values).Scan(source))
		got := make([]string, len(values))
		for index, value := range values {
			if value == nil {
				got[index] = "<nil>"
			} else {
				got[index] = *value
			}
		}
		require.Equal(t, []string{"a", "", "NULL", "<nil>", "with space", `q"uote`, `back\slash`, "{brace}", "com,ma", " lead", "ünï", "tab\tx", "<nil>"}, got)
	}
}

func TestScanArrayReadsEveryElementType(t *testing.T) {
	t.Parallel()

	var integers []int64
	require.NoError(t, sglScanArray(&integers).Scan("{9223372036854775807,-9223372036854775808}"))
	require.Equal(t, []int64{math.MaxInt64, math.MinInt64}, integers)

	var narrow []*int16
	require.NoError(t, sglScanArray(&narrow).Scan("{-32768,NULL}"))
	require.Equal(t, int16(-32768), *narrow[0])
	require.Nil(t, narrow[1])

	var words []int32
	require.NoError(t, sglScanArray(&words).Scan("{1,2}"))
	require.Equal(t, []int32{1, 2}, words)

	var plain []int
	require.NoError(t, sglScanArray(&plain).Scan("{3}"))
	require.Equal(t, []int{3}, plain)

	var doubles []float64
	require.NoError(t, sglScanArray(&doubles).Scan("{1.5,NaN,Infinity,-Infinity,1e+300,0.1,-0,5e-324}"))
	require.Equal(t, 1.5, doubles[0])
	require.True(t, math.IsNaN(doubles[1]))
	require.True(t, math.IsInf(doubles[2], 1))
	require.True(t, math.IsInf(doubles[3], -1))
	require.Equal(t, []float64{1e300, 0.1}, doubles[4:6])
	require.True(t, math.Signbit(doubles[6]))
	require.Equal(t, 5e-324, doubles[7])

	var singles []*float32
	require.NoError(t, sglScanArray(&singles).Scan("{0.1,3.4e+38,NULL}"))
	require.Equal(t, float32(0.1), *singles[0])
	require.Equal(t, float32(3.4e38), *singles[1])
	require.Nil(t, singles[2])

	var flags []*bool
	require.NoError(t, sglScanArray(&flags).Scan("{t,f,NULL}"))
	require.True(t, *flags[0])
	require.False(t, *flags[1])
	require.Nil(t, flags[2])
}

func TestScanArrayReadsEmptyNullAndBoundedArrays(t *testing.T) {
	t.Parallel()

	values := []string{"old"}
	require.NoError(t, sglScanArray(&values).Scan(nil))
	require.Nil(t, values)

	require.NoError(t, sglScanArray(&values).Scan("{}"))
	require.NotNil(t, values)
	require.Empty(t, values)

	require.NoError(t, sglScanArray(&values).Scan("[2:3]={a,b}"))
	require.Equal(t, []string{"a", "b"}, values)

	require.NoError(t, sglScanArray(&values).Scan(`{  a  , "b" }`))
	require.Equal(t, []string{"a", "b"}, values)
}

func TestScanArrayRefusesWhatItsJSONWouldNotRead(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		read   func() error
		reason string
	}{
		"a NULL element of a value type": {func() error { var v []string; return sglScanArray(&v).Scan("{a,NULL}") }, "element 1: converting NULL to string is unsupported"},
		"two dimensions":                 {func() error { var v []string; return sglScanArray(&v).Scan("{{a,b},{c,d}}") }, "one-dimensional"},
		"bounded two dimensions":         {func() error { var v []string; return sglScanArray(&v).Scan("[1:1][1:2]={{a,b}}") }, "one-dimensional"},
		"an integer too wide":            {func() error { var v []int16; return sglScanArray(&v).Scan("{40000}") }, "element 0"},
		"a boolean":                      {func() error { var v []bool; return sglScanArray(&v).Scan("{yes}") }, "invalid boolean"},
		"no braces":                      {func() error { var v []string; return sglScanArray(&v).Scan("a,b") }, "one-dimensional"},
		"an open quote":                  {func() error { var v []string; return sglScanArray(&v).Scan(`{"a}`) }, "one-dimensional"},
		"text after a quote":             {func() error { var v []string; return sglScanArray(&v).Scan(`{"a"b}`) }, "one-dimensional"},
		"an empty element":               {func() error { var v []string; return sglScanArray(&v).Scan(`{a,,b}`) }, "one-dimensional"},
		"another driver value":           {func() error { var v []string; return sglScanArray(&v).Scan(42) }, "cannot read int"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := testCase.read()
			require.ErrorContains(t, err, testCase.reason)
			require.ErrorContains(t, err, "sgl: read array column")
		})
	}

	values := []int64{1}
	require.Error(t, sglScanArray(&values).Scan("{1,x}"))
	require.Nil(t, values, "the slice is left nil when the array cannot be read")
}

func TestScanArrayAllocatesAFixedNumberOfTimesPerRow(t *testing.T) {
	raw := []byte("{" + strings.Repeat("alpha,", 99) + "alpha}")
	allocations := testing.AllocsPerRun(50, func() {
		var values []*string
		if err := sglScanArray(&values).Scan(raw); err != nil {
			panic(err)
		}
	})

	require.LessOrEqual(t, allocations, 5.0)
}

func BenchmarkScanArray(b *testing.B) {
	raw := []byte("{" + strings.Repeat("alpha,", 9) + "alpha}")
	b.Run("Text", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var values []*string
			if err := sglScanArray(&values).Scan(raw); err != nil {
				b.Fatal(err)
			}
		}
	})
	json := []byte(`["` + strings.Repeat(`alpha","`, 9) + `alpha"]`)
	b.Run("JSON", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var values []*string
			if err := sglScanInto(&values).Scan(json); err != nil {
				b.Fatal(err)
			}
		}
	})
}
