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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListWritesOnePlaceholderPerElement(t *testing.T) {
	t.Parallel()

	query := "SELECT id FROM t WHERE a = #1 AND id IN (#2) AND b = #3"
	mark := sglListMark{Empty: "IN (SELECT NULL WHERE 0)", Operator: 37, Start: 40, End: 44}

	testCases := []struct {
		name  string
		want  string
		first int
		count int
	}{
		{name: "many elements are numbered from the list's first number", first: 2, count: 3, want: "SELECT id FROM t WHERE a = #1 AND id IN (#2,#3,#4) AND b = #3"},
		{name: "one element keeps one placeholder", first: 2, count: 1, want: "SELECT id FROM t WHERE a = #1 AND id IN (#2) AND b = #3"},
		{name: "a list after another is numbered after its elements", first: 5, count: 2, want: "SELECT id FROM t WHERE a = #1 AND id IN (#5,#6) AND b = #3"},
		{name: "an empty slice writes the empty test over the operator and the list", first: 2, count: 0, want: "SELECT id FROM t WHERE a = #1 AND id IN (SELECT NULL WHERE 0) AND b = #3"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglSplice(query, sglList(mark, testCase.first, testCase.count)))
		})
	}
}

func TestListWritesTheEmptyExclusionForNotIn(t *testing.T) {
	t.Parallel()

	query := "SELECT id FROM t WHERE id NOT IN (#1)"
	mark := sglListMark{Empty: "NOT IN (SELECT NULL WHERE 0)", Operator: 26, Start: 33, End: 37}

	require.Equal(t, "SELECT id FROM t WHERE id NOT IN (SELECT NULL WHERE 0)", sglSplice(query, sglList(mark, 1, 0)))
	require.Equal(t, "SELECT id FROM t WHERE id NOT IN (#1,#2)", sglSplice(query, sglList(mark, 1, 2)))
}
