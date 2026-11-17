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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzznative"
)

func TestBlobAgnosticMatchesAnEmptyBlobWithANilOne(t *testing.T) {
	t.Parallel()
	modernc := readResult([]string{"integer:1", "nil-uint8s"})
	mattn := readResult([]string{"integer:1", "bytes:"})

	require.Equal(t, blobAgnostic(mattn), blobAgnostic(modernc), "modernc.org/sqlite scans an empty BLOB as a nil []byte")
	require.Equal(t, [][]string{{"integer:1", "nil-uint8s"}}, modernc[0].Rows, "the observation itself is left as it was")
	require.NotEqual(t, blobAgnostic(readResult([]string{"integer:1", "bytes:00"})), blobAgnostic(mattn), "a blob with a byte stays apart")
}

func readResult(row []string) []fuzznative.ModeResult {
	return []fuzznative.ModeResult{{Method: "Read", Command: "many", Error: "", Rows: [][]string{row}, NoRows: false}}
}
