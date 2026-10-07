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

package ast_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestDataChangeIncludesEveryStatementOfAnotherSet(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		set   ast.DataChange
		other ast.DataChange
		want  bool
	}{
		{name: "a statement in a set holding it", set: ast.DataChangeInsert | ast.DataChangeDelete, other: ast.DataChangeDelete,
			want: true},
		{name: "a statement the set lacks", set: ast.DataChangeInsert, other: ast.DataChangeUpdate, want: false},
		{name: "several statements all held", set: ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete,
			other: ast.DataChangeInsert | ast.DataChangeUpdate, want: true},
		{name: "several statements one of them missing", set: ast.DataChangeInsert | ast.DataChangeDelete,
			other: ast.DataChangeInsert | ast.DataChangeMerge, want: false},
		{name: "a statement of unknown kind", set: ast.DataChangeInsert | ast.DataChangeUpdate, other: 0, want: false},
		{name: "a statement in the empty set", set: 0, other: ast.DataChangeMerge, want: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.set.Includes(testCase.other))
		})
	}
}

func TestDataChangeStringListsTheStatementsAsKeywords(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		change ast.DataChange
		want   string
	}{
		{name: "the empty set", change: 0, want: "none"},
		{name: "one statement", change: ast.DataChangeMerge, want: "MERGE"},
		{name: "two statements written out of order", change: ast.DataChangeDelete | ast.DataChangeInsert, want: "INSERT, DELETE"},
		{name: "every statement", change: ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete | ast.DataChangeMerge,
			want: "INSERT, UPDATE, DELETE, MERGE"},
		{name: "a bit no statement names", change: ast.DataChange(1 << 7), want: "none"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.change.String())
		})
	}
}
