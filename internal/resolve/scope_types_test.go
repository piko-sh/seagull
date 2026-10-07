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

package resolve

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestScopeRulesRecogniseRowIDAliases(t *testing.T) {
	t.Parallel()

	sensitive := ScopeRules{
		RowIDType:    sqlmodel.NewIntegerType("ROWNUM", 64, false),
		RowIDAliases: []string{"rownum"},
		Identifiers:  sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingUpper, Insensitive: false},
	}
	tests := []struct {
		name  string
		rules ScopeRules
		input string
		want  bool
	}{
		{name: "rowid lowercase", rules: testScopeRules, input: "rowid", want: true},
		{name: "ROWID uppercase", rules: testScopeRules, input: "ROWID", want: true},
		{name: "_rowid_ lowercase", rules: testScopeRules, input: "_rowid_", want: true},
		{name: "OID uppercase", rules: testScopeRules, input: "OID", want: true},
		{name: "id is not implicit rowid", rules: testScopeRules, input: "id", want: false},
		{name: "row_id with underscore is not implicit rowid", rules: testScopeRules, input: "row_id", want: false},
		{name: "empty string is not implicit rowid", rules: testScopeRules, input: "", want: false},
		{name: "no aliases means no implicit column", rules: ScopeRules{}, input: "rowid", want: false},
		{name: "a folded alias matches the folded name", rules: sensitive, input: "ROWNUM", want: true},
		{name: "a case-sensitive dialect compares exactly", rules: sensitive, input: "rownum", want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.rules.isRowID(testCase.input))
		})
	}
}
