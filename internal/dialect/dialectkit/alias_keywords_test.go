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

func TestAliasKeywordsRefuseOnlyTheListedWords(t *testing.T) {
	t.Parallel()

	keywords := dialectkit.NewAliasKeywords("from\twhere", " order  limit\n")
	testCases := []struct {
		name    string
		word    string
		afterAs bool
		bare    bool
	}{
		{name: "a word refused after AS", word: "FROM", afterAs: true, bare: false},
		{name: "a word refused bare", word: "LIMIT", afterAs: false, bare: true},
		{name: "a word refused nowhere", word: "VALUE", afterAs: false, bare: false},
		{name: "a quoted name", word: "", afterAs: false, bare: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.afterAs, keywords.RefusesAfterAs(testCase.word))
			assert.Equal(t, testCase.bare, keywords.RefusesBare(testCase.word))
		})
	}
}

func TestRefusalMessageNamesTheKeyword(t *testing.T) {
	t.Parallel()

	assert.Contains(t, dialectkit.RefusalMessage("FROM"), "FROM")
}
