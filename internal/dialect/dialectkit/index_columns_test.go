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

package dialectkit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIndexColumnNamesKeepPlainKeysAndExcludeExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "ordered keys", input: []string{"category", ",", "id", "DESC"}, want: []string{"category", "id"}},
		{name: "collated key", input: []string{"name", "COLLATE", "nocase", "ASC", "NULLS", "LAST"}, want: []string{"name"}},
		{name: "function commas", input: []string{"coalesce", "(", "category", ",", "id", ")", ",", "score"}, want: []string{"score"}},
		{name: "nested expression", input: []string{"(", "id", "+", "abs", "(", "score", ")", ")", ",", "category"}, want: []string{"category"}},
		{name: "arithmetic", input: []string{"id", "+", "score"}, want: nil},
		{name: "subscript", input: []string{"items", "[", "id", "]"}, want: nil},
		{name: "missing collation", input: []string{"id", "COLLATE"}, want: nil},
		{name: "quoted keyword", input: []string{`"DESC"`, ",", `"("`}, want: []string{"DESC", "("}},
		{name: "empty", input: nil, want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var tokens []Token[string]
			for _, raw := range test.input {
				kind := "word"
				if strings.Contains("(),[]+", raw) && len(raw) == 1 {
					kind = raw
				}
				tokens = append(tokens, Token[string]{Kind: kind, Raw: raw, Value: strings.Trim(raw, `"`), Quoted: strings.HasPrefix(raw, `"`)})
			}
			assert.Equal(t, test.want, IndexColumnNames(tokens, "word", ","))
		})
	}
}
