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
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func TestReservedNamesRefuseOnlyTheListedWords(t *testing.T) {
	t.Parallel()

	names := dialectkit.NewReservedNames("select check", "table\tuser")
	testCases := []struct {
		name   string
		word   string
		column bool
		table  bool
	}{
		{name: "a word refused for a column", word: "SELECT", column: true, table: false},
		{name: "a word refused for a table", word: "USER", column: false, table: true},
		{name: "a word refused for neither", word: "NAME", column: false, table: false},
		{name: "a quoted name", word: "", column: false, table: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.column, names.RefusesColumn(testCase.word))
			assert.Equal(t, testCase.table, names.RefusesTable(testCase.word))
		})
	}
}

func TestRefusesColumnDefinitionLeavesAConstraintToItsParser(t *testing.T) {
	t.Parallel()

	names := dialectkit.NewReservedNames("select check primary", "")
	for _, word := range []string{"CHECK", "PRIMARY"} {
		assert.False(t, names.RefusesColumnDefinition(word, true), word)
		assert.True(t, names.RefusesColumnDefinition(word, false), word)
	}
	assert.True(t, names.RefusesColumnDefinition("SELECT", true))
	assert.False(t, names.RefusesColumnDefinition("NAME", false))
	assert.False(t, names.RefusesColumnDefinition("", false))
}

func TestReservedNameMessageNamesTheKeywordAndKind(t *testing.T) {
	t.Parallel()

	message := dialectkit.ReservedNameMessage("USER", "table")
	assert.Contains(t, message, "USER")
	assert.Contains(t, message, "table")
}
