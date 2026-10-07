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

package dialectkit_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func TestReadParameterNumberReadsDigitsUpToTheMaximum(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		input      string
		wantToken  string
		wantEnd    int
		wantNumber int
		wantErr    bool
	}{
		{name: "a numbered placeholder", input: "$12 ", wantEnd: 3, wantNumber: 12},
		{name: "the maximum", input: "$65535", wantEnd: 6, wantNumber: 65535},
		{name: "no digits", input: "$x", wantEnd: 1, wantNumber: 0},
		{name: "past the maximum", input: "$65536", wantErr: true, wantToken: "$6553"},
		{name: "a run of digits far past the maximum", input: "$" + strings.Repeat("9", 400), wantErr: true,
			wantToken: "$9999"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			end, number, err := dialectkit.ReadParameterNumber(testCase.input, 0, 1, 65535)

			if testCase.wantErr {
				var parseError *dialectkit.ParseError
				require.ErrorAs(t, err, &parseError)
				require.Equal(t, testCase.wantToken, parseError.Token)
				require.Equal(t, 0, parseError.Position)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.wantEnd, end)
			require.Equal(t, testCase.wantNumber, number)
		})
	}
}
