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

func TestConflictActionString_NamesEachAction(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		want   string
		action ast.ConflictAction
	}{
		{name: "the zero value is unrecorded", action: ast.ConflictActionUnrecorded, want: "unrecorded"},
		{name: "no conflict clause", action: ast.ConflictActionNone, want: "none"},
		{name: "DO NOTHING", action: ast.ConflictActionDoNothing, want: "DO NOTHING"},
		{name: "DO UPDATE", action: ast.ConflictActionDoUpdate, want: "DO UPDATE"},
		{name: "an undeclared value gives its number", action: ast.ConflictAction(9), want: "ConflictAction(9)"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.action.String())
		})
	}
}

func TestRawQueryAnalysis_LeavesTheConflictActionUnrecordedByDefault(t *testing.T) {
	t.Parallel()

	analysis := &ast.RawQueryAnalysis{}

	assert.Equal(t, ast.ConflictActionUnrecorded, analysis.ConflictAction)
}
