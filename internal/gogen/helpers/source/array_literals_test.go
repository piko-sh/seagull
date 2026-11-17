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
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArrayLiteralWritesEachElementType(t *testing.T) {
	t.Parallel()

	word := "it's"
	seven := int64(7)
	yes := true
	require.Equal(t, "{}", sglArrayLiteral([]int64(nil)))
	require.Equal(t, "{}", sglArrayLiteral([]string{}))
	require.Equal(t, "{9223372036854775807,-9223372036854775808}", sglArrayLiteral([]int64{math.MaxInt64, math.MinInt64}))
	require.Equal(t, "{-2147483648,1}", sglArrayLiteral([]int32{math.MinInt32, 1}))
	require.Equal(t, "{-32768}", sglArrayLiteral([]int16{math.MinInt16}))
	require.Equal(t, "{3}", sglArrayLiteral([]int{3}))
	require.Equal(t, "{true,false}", sglArrayLiteral([]bool{true, false}))
	require.Equal(t, `{"","NULL","a\"b","c\\d","{,}"," x "}`, sglArrayLiteral([]string{"", "NULL", `a"b`, `c\d`, "{,}", " x "}))
	require.Equal(t, `{"it's",NULL}`, sglArrayLiteral([]*string{&word, nil}))
	require.Equal(t, "{7,NULL}", sglArrayLiteral([]*int64{&seven, nil}))
	require.Equal(t, "{NULL,true}", sglArrayLiteral([]*bool{nil, &yes}))
}

func TestArrayLiteralReadsBackAsTheSlice(t *testing.T) {
	t.Parallel()

	texts := []string{"", "NULL", "null", `"`, `\`, `\"`, "a,b", "{x}", " lead", "trail ", "ünï", "tab\tnew\nline"}
	pointers := make([]*string, 0, len(texts)+1)
	for index := range texts {
		pointers = append(pointers, &texts[index])
	}
	pointers = append(pointers, nil)
	var read []*string
	require.NoError(t, sglScanArray(&read).Scan(sglArrayLiteral(pointers)))
	require.Equal(t, pointers, read)

	integers := []int64{0, -1, math.MaxInt64, math.MinInt64}
	var readIntegers []int64
	require.NoError(t, sglScanArray(&readIntegers).Scan(sglArrayLiteral(integers)))
	require.Equal(t, integers, readIntegers)
}

func TestArrayLiteralAllocatesOnceForIntegers(t *testing.T) {
	values := make([]int64, 50)
	for index := range values {
		values[index] = int64(1001 + index*7)
	}
	words := []string{"alpha", "be\"ta", "gamma"}
	require.LessOrEqual(t, testing.AllocsPerRun(50, func() { _ = sglArrayLiteral(values) }), 1.0)
	require.LessOrEqual(t, testing.AllocsPerRun(50, func() { _ = sglArrayLiteral(words) }), 1.0)
}
