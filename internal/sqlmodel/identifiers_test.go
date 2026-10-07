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

package sqlmodel

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentifierCaseFolds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		rules   IdentifierCase
		written string
		folded  string
		key     string
		quoted  bool
	}{
		{name: "lower folding lowers an unquoted name", rules: IdentifierCase{Unquoted: IdentifierFoldingLower}, written: "Users", folded: "users", key: "users"},
		{name: "lower folding keeps a quoted name", rules: IdentifierCase{Unquoted: IdentifierFoldingLower}, written: "Users", quoted: true, folded: "Users", key: "Users"},
		{name: "upper folding raises an unquoted name", rules: IdentifierCase{Unquoted: IdentifierFoldingUpper}, written: "dbo", folded: "DBO", key: "DBO"},
		{name: "no folding keeps the name", rules: IdentifierCase{Unquoted: IdentifierFoldingNone}, written: "Events", folded: "Events", key: "Events"},
		{name: "an insensitive dialect keys by lower case", rules: IdentifierCase{Unquoted: IdentifierFoldingNone, Insensitive: true}, written: "Events", quoted: true, folded: "Events", key: "events"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			folded := testCase.rules.Fold(testCase.written, testCase.quoted)
			require.Equal(t, testCase.folded, folded)
			require.Equal(t, testCase.key, testCase.rules.Key(folded))
		})
	}
}

func TestIdentifierCaseEqual(t *testing.T) {
	t.Parallel()

	sensitive := IdentifierCase{Unquoted: IdentifierFoldingLower, Insensitive: false}
	insensitive := IdentifierCase{Unquoted: IdentifierFoldingNone, Insensitive: true}

	require.True(t, sensitive.Equal("users", "users"))
	require.False(t, sensitive.Equal("Users", "users"))
	require.True(t, insensitive.Equal("Users", "USERS"))
	require.Equal(t, "lower", IdentifierFoldingLower.String())
	require.Equal(t, "IdentifierFolding(9)", IdentifierFolding(9).String())
}
