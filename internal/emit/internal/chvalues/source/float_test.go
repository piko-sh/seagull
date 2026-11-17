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
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPositionalFloatPreservesExactTextAndNulls(t *testing.T) {
	t.Parallel()
	minimum := math.SmallestNonzeroFloat64
	require.Equal(t, "1", sglClickHouseFloatValue(minimum, "f", false))
	require.Equal(t, "1", sglClickHouseFloatValue(&minimum, "f", false))
	require.Equal(t, "NULL", sglClickHouseFloatValue((*float64)(nil), "f", false))
	require.Equal(t, "[1,9223372036854775809]", sglClickHouseFloatValue([]float64{minimum, -minimum}, "[f]", false))
	require.Equal(t, "[NULL,1]", sglClickHouseFloatValue([]*float64{nil, &minimum}, "[f]", false))
	require.Equal(t, "[]", sglClickHouseFloatValue([]float64(nil), "[f]", false))
}

func TestFloatWireEncodingPreservesAllIEEEBits(t *testing.T) {
	t.Parallel()
	for _, bits := range []uint64{0, 1, 1 << 63, 1<<63 | 1, 0x7fefffffffffffff, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000000001} {
		value := math.Float64frombits(bits)
		require.Equal(t, strconv.FormatUint(bits, 10), sglClickHouseFloatValue(value, "f", false))
		require.Equal(t, strconv.FormatUint(bits, 10), sglClickHouseFloatValue(value, "f", true))
	}
	require.Equal(t, "NULL", sglClickHouseFloatValue((*float64)(nil), "f", true))
}
