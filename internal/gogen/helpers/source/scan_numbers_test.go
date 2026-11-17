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

func TestScanNumberReadsDriverValuesExactly(t *testing.T) {
	t.Parallel()

	var small int16
	require.NoError(t, sglScanNumber(&small).Scan(int64(-300)))
	require.Equal(t, int16(-300), small)
	require.Error(t, sglScanNumber(&small).Scan(int64(math.MaxInt32)), "an integer that does not fit is refused")
	require.NoError(t, sglScanNumber(&small).Scan([]byte("42")), "text is converted as database/sql converts it")
	require.Equal(t, int16(42), small)
	require.Error(t, sglScanNumber(&small).Scan(nil), "NULL cannot be read into a number")

	var whole int32
	require.Error(t, sglScanNumber(&whole).Scan(1.5), "a fraction is refused rather than truncated")
	require.NoError(t, sglScanNumber(&whole).Scan(int32(7)))
	require.Equal(t, int32(7), whole)

	var single float32
	require.NoError(t, sglScanNumber(&single).Scan(float64(float32(0.1))))
	require.Equal(t, float32(0.1), single)
	require.NoError(t, sglScanNumber(&single).Scan(0.1), "a float64 rounds to the nearest float32, as database/sql rounds it")
	require.Equal(t, float32(0.1), single)
}

func TestScanNumberAllocatesNothingForAnExactValue(t *testing.T) {
	var target int32
	scanner := sglScanNumber(&target)
	var source any = int64(123456)
	allocations := testing.AllocsPerRun(100, func() {
		var destination any = scanner
		if err := destination.(interface{ Scan(any) error }).Scan(source); err != nil {
			t.Fatal(err)
		}
	})
	require.Zero(t, allocations)
}
