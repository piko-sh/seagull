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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckBindsAcceptsACountAtTheLimit(t *testing.T) {
	t.Parallel()

	require.NoError(t, sglCheckBinds(sglMaxBindVariables))
	require.NoError(t, sglCheckBinds(0))
}

func TestCheckBindsRejectsACountPastTheLimit(t *testing.T) {
	t.Parallel()

	err := sglCheckBinds(sglMaxBindVariables + 1)

	require.ErrorIs(t, err, ErrSglTooManyBindVariables)
	require.ErrorContains(t, err, fmt.Sprintf("%d bind variables exceeds the limit of %d", sglMaxBindVariables+1, sglMaxBindVariables))
}

func TestTooManyBindVariablesIsASentinelCallersCanMatchThroughWrapping(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("running the report: %w", ErrSglTooManyBindVariables)

	require.ErrorIs(t, wrapped, ErrSglTooManyBindVariables)
	require.EqualError(t, ErrSglTooManyBindVariables, "sgl: too many bind variables")
	require.False(t, errors.Is(errors.New("sgl: too many bind variables"), ErrSglTooManyBindVariables))
}

func TestSpliceMakesItsEditsAtTheOffsetsOfTheQuery(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		query string
		want  string
		edits [][]sglEdit
	}{
		{
			name:  "no edit leaves the query as it is",
			query: "SELECT id FROM t",
			edits: nil,
			want:  "SELECT id FROM t",
		},
		{
			name:  "a replacement writes its text over its range",
			query: "SELECT id FROM t WHERE id IN (#1)",
			edits: [][]sglEdit{{{Text: "(#1,#2)", Start: 29, End: 33}}},
			want:  "SELECT id FROM t WHERE id IN (#1,#2)",
		},
		{
			name:  "an insertion writes its text at its offset",
			query: "SELECT id FROM t LIMIT 3",
			edits: [][]sglEdit{{{Text: " ORDER BY id", Start: 16, End: 16}}},
			want:  "SELECT id FROM t ORDER BY id LIMIT 3",
		},
		{
			name:  "edits across groups keep their offsets in the query as written",
			query: "SELECT id FROM t WHERE a = 1 LIMIT 3",
			edits: [][]sglEdit{
				{{Text: "(", Start: 23, End: 23}, {Text: ") AND b = 2", Start: 28, End: 28}},
				nil,
				{{Text: " ORDER BY id", Start: 28, End: 28}},
			},
			want: "SELECT id FROM t WHERE (a = 1) AND b = 2 ORDER BY id LIMIT 3",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglSplice(testCase.query, testCase.edits...))
		})
	}
}
