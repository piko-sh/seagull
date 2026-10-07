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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func TestTokenKeywordRejectsQuotedIdentifiers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		token       dialectkit.Token[testKind]
		keyword     string
		wantMatch   bool
		wantKeyword string
	}{
		{name: "bare word matches ignoring case", token: word("select", 0), keyword: "SELECT", wantMatch: true, wantKeyword: "SELECT"},
		{name: "quoted word never matches", token: quotedWord("where", 0), keyword: "WHERE", wantMatch: false, wantKeyword: ""},
		{name: "other kind never matches", token: symbol("(", 0), keyword: "(", wantMatch: false, wantKeyword: ""},
		{name: "different word does not match", token: word("from", 0), keyword: "WHERE", wantMatch: false, wantKeyword: "FROM"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.wantMatch, testCase.token.IsKeyword(kindWord, testCase.keyword))
			require.Equal(t, testCase.wantKeyword, testCase.token.Keyword(kindWord))
		})
	}
}
