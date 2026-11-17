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

// IndexColumnNames extracts plain keys without mistaking expressions for columns.
//
// Takes tokens ([]Token[K]) which holds the contents of a balanced index key list.
// Takes identifier (K) which identifies bare and quoted column names.
// Takes comma (K) which separates keys at the outer depth.
//
// Returns []string which retains the order of supported plain-column keys.
func IndexColumnNames[K comparable](tokens []Token[K], identifier, comma K) []string {
	var columns []string
	start, depth := 0, 0
	for index, token := range tokens {
		if depth == 0 && token.Kind == comma {
			columns = appendIndexColumn(columns, tokens[start:index], identifier)
			start = index + 1
		}
		switch token.Raw {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
		}
	}
	return appendIndexColumn(columns, tokens[start:], identifier)
}

// appendIndexColumn records a bare key with optional ordering or collation.
//
// Takes columns ([]string) which accumulates plain key names.
// Takes tokens ([]Token[K]) which holds one key expression.
// Takes identifier (K) which identifies column names.
//
// Returns []string which includes the key when its expression is a simple reference.
func appendIndexColumn[K comparable](columns []string, tokens []Token[K], identifier K) []string {
	if len(tokens) == 0 || tokens[0].Kind != identifier {
		return columns
	}
	for index := 1; index < len(tokens); index++ {
		switch tokens[index].Keyword(identifier) {
		case "ASC", "DESC":
		case "COLLATE", "NULLS":
			index++
			if index >= len(tokens) || tokens[index].Kind != identifier {
				return columns
			}
		default:
			return columns
		}
	}
	return append(columns, tokens[0].Value)
}
