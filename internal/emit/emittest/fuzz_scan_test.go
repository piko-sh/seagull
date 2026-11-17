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

package emittest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestFuzzScanOrderDetectsReorderedAndRepeatedDestinations(t *testing.T) {
	const source = `package generated
    type row struct { ID int64; Score int64 }
    type reader interface { Scan(...any) error }
    func read(rows reader) (row, error) {
        var result row
        err := rows.Scan(&result.ID, &result.Score)
        return result, err
    }`
	testCases := []struct {
		name      string
		arguments string
		ordered   bool
	}{
		{name: "fields in order", arguments: "&result.ID, &result.Score", ordered: true},
		{name: "fields reordered", arguments: "&result.Score, &result.ID", ordered: false},
		{name: "a field repeated", arguments: "&result.ID, &result.ID", ordered: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			text := strings.Replace(source, "&result.ID, &result.Score", testCase.arguments, 1)
			checked := typeCheck([]sqlmodel.GeneratedFile{{Name: "query.go", Content: []byte(text)}})
			require.Empty(t, checked.problems)
			problems := fuzzScanOrderProblems(checked)
			if testCase.ordered {
				require.Empty(t, problems)
				return
			}
			require.NotEmpty(t, problems)
		})
	}
}
