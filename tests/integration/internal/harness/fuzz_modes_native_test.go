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

package harness

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestShortenedCutsOnCharacterBoundaries(t *testing.T) {
	t.Parallel()
	require.Equal(t, "short", shortened("short"))
	long := shortened(strings.Repeat("é", messageBound+1))
	require.True(t, utf8.ValidString(long))
	require.Equal(t, messageBound+len("..."), utf8.RuneCountInString(long))
}

func TestRuntimeVariantSelectsEachMode(t *testing.T) {
	t.Parallel()
	base := runtimeBase([]byte{1, 2})
	require.Len(t, base, runtimeModeByte+1, "the mode byte is padded")
	require.Equal(t, modeSQL, shapeMode(int(runtimeVariant(base, modeSQL)[runtimeModeByte])))
	require.Equal(t, modePrepared, shapeMode(int(runtimeVariant(base, modePrepared)[runtimeModeByte])))
	require.Equal(t, modeNative, shapeMode(int(runtimeVariant(base, modeNative)[runtimeModeByte])))
}
