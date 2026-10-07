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

package emitkit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestScopeClaim(t *testing.T) {
	t.Parallel()

	scope := newNameScope("queries", "ctx")
	claims := []struct {
		name     string
		expected string
	}{
		{name: "row", expected: "row"},
		{name: "ctx", expected: "ctx2"},
		{name: "ctx", expected: "ctx3"},
		{name: "row", expected: "row2"},
		{name: "queries", expected: "queries2"},
		{name: "ctx2", expected: "ctx22"},
	}

	for _, claim := range claims {
		require.Equal(t, claim.expected, scope.claim(claim.name), "claiming %q", claim.name)
	}
}

func TestScopesAreIndependent(t *testing.T) {
	t.Parallel()

	first := newNameScope()
	second := newNameScope()

	require.Equal(t, "name", first.claim("name"))
	require.Equal(t, "name", second.claim("name"))
	require.Equal(t, "name2", first.claim("name"))
}

func TestScopeClaimPassesOverNamesItsParentHolds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		parent []string
		claims []string
		want   []string
	}{
		{name: "a name the parent holds is numbered", parent: []string{"row"}, claims: []string{"row", "row"}, want: []string{"row2", "row3"}},
		{
			name: "a numbered name the parent holds is passed over", parent: []string{"row", "row2", "row4"},
			claims: []string{"row", "row", "row"}, want: []string{"row3", "row5", "row6"},
		},
		{name: "a name the parent leaves free keeps its name", parent: []string{"ctx"}, claims: []string{"row", "row"}, want: []string{"row", "row2"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := newNameScope()
			scope.parent = newNameScope(testCase.parent...)
			claimed := make([]string, 0, len(testCase.claims))
			for _, name := range testCase.claims {
				claimed = append(claimed, scope.claim(name))
			}
			require.Equal(t, testCase.want, claimed)
		})
	}
}

func TestScopeClaimTakesTimeLinearInTheNamesThatRepeat(t *testing.T) {
	t.Parallel()

	claimAll := func(count int) func() {
		return func() {
			scope := newNameScope()
			for range count {
				scope.claim("x")
			}
		}
	}

	scaling.Linear(t, claimAll(4_000), claimAll(16_000))
}
