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

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestUpsertClausesNameTheConflictActionOfTheirStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		resolution string
		clauses    []ast.ConflictAction
		want       ast.ConflictAction
	}{
		{name: "no resolution and no clause", resolution: "", clauses: nil, want: ast.ConflictActionNone},
		{name: "a resolution that aborts", resolution: "ABORT", clauses: nil, want: ast.ConflictActionNone},
		{name: "IGNORE alone", resolution: conflictResolutionIgnore, clauses: nil, want: ast.ConflictActionDoNothing},
		{name: "REPLACE alone", resolution: conflictResolutionReplace, clauses: nil, want: ast.ConflictActionDoUpdate},
		{name: "a DO NOTHING clause", resolution: "", clauses: []ast.ConflictAction{ast.ConflictActionDoNothing},
			want: ast.ConflictActionDoNothing},
		{name: "a DO UPDATE clause", resolution: "", clauses: []ast.ConflictAction{ast.ConflictActionDoUpdate},
			want: ast.ConflictActionDoUpdate},
		{name: "a DO UPDATE clause then a DO NOTHING clause", resolution: "",
			clauses: []ast.ConflictAction{ast.ConflictActionDoUpdate, ast.ConflictActionDoNothing}, want: ast.ConflictActionDoNothing},
		{name: "REPLACE with a DO NOTHING clause", resolution: conflictResolutionReplace,
			clauses: []ast.ConflictAction{ast.ConflictActionDoNothing}, want: ast.ConflictActionDoNothing},
		{name: "IGNORE with a DO UPDATE clause", resolution: conflictResolutionIgnore,
			clauses: []ast.ConflictAction{ast.ConflictActionDoUpdate}, want: ast.ConflictActionDoNothing},
		{name: "a clause whose action was not read", resolution: "",
			clauses: []ast.ConflictAction{ast.ConflictActionUnrecorded}, want: ast.ConflictActionNone},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var upsert upsertClauses
			for _, action := range testCase.clauses {
				upsert.add(action)
			}

			assert.Equal(t, testCase.want, upsert.conflictAction(testCase.resolution))
		})
	}
}
