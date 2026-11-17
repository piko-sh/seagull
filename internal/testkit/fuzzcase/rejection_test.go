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

package fuzzcase

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRejectionDecoderKeepsStableOperationsAndBounds(t *testing.T) {
	codes := []string{"Q020", "Q006", "Q012", "Q028", "Q018", "Q017"}
	require.Equal(t, 128, rejectionVersion, "changed the stored rejection version")
	for engine := range capabilities {
		for index, seed := range RejectionSeeds() {
			rejection, ok := DecodeRejection(engine, seed)
			require.True(t, ok, "rejected seed %s/%d", engine, index)
			require.Equal(t, codes[index], rejection.Code, "invalid rejection seed %s/%d", engine, index)
			require.NotEmpty(t, rejection.SQL, "invalid rejection seed %s/%d", engine, index)
			again, _ := DecodeRejection(engine, seed)
			require.Equal(t, rejection, again, "rejection decoding changed across calls")
			_, ok = Decode(engine, seed)
			require.False(t, ok, "positive decoder accepted a deliberate rejection")
			short, ok := DecodeRejection(engine, seed[:1])
			require.True(t, ok, "shortening did not select the simple default")
			require.Equal(t, codes[0], short.Code, "shortening did not select the simple default")
		}
	}
	for _, data := range [][]byte{nil, {}, {1}, {2}, bytes.Repeat([]byte{128}, MaxBytes+1)} {
		_, ok := DecodeRejection("postgres", data)
		require.False(t, ok, "accepted an invalid rejection envelope")
	}
	_, ok := DecodeRejection("unknown", []byte{128})
	require.False(t, ok, "accepted an unsupported engine")
}
