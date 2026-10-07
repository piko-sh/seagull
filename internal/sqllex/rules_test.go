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

package sqllex_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqllex"
)

func TestRuleEnumerationNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value    interface{ String() string }
		expected string
	}{
		{value: sqllex.EscapeDoubled, expected: "doubled"},
		{value: sqllex.EscapeBackslash, expected: "backslash"},
		{value: sqllex.EscapeDoubledOrBackslash, expected: "doubled-or-backslash"},
		{value: sqllex.EscapeMode(200), expected: "unknown"},
		{value: sqllex.DoubleQuoteIdentifier, expected: "identifier"},
		{value: sqllex.DoubleQuoteString, expected: "string"},
		{value: sqllex.DoubleQuoteMeaning(200), expected: "unknown"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.expected, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, testCase.value.String())
		})
	}
}
