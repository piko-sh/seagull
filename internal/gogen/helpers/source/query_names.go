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

import "strings"

// queryNamePrefixed enables matching generated SQL by configured prefixes.
//
//sgl:inline prefixed
const queryNamePrefixed = true

// queryNameMap associates exact generated SQL with query names.
//
//sgl:data names
var queryNameMap = map[string]string{}

// queryNamePrefixes holds SQL prefixes for queries with variable-length text.
//
//sgl:when prefixed
//sgl:data prefixes
var queryNamePrefixes = []string{}

// queryNamePrefixNames holds query names in the order of queryNamePrefixes.
//
//sgl:when prefixed
//sgl:data prefixNames
var queryNamePrefixNames = []string{}

// QueryName identifies generated SQL by exact text or a configured prefix.
//
// Takes query (string) which is the SQL text passed to the driver.
//
// Returns string which is the generated query name, or empty when no query matches.
func QueryName(query string) string {
	if !queryNamePrefixed {
		return queryNameMap[query]
	}
	if name, ok := queryNameMap[query]; ok {
		return name
	}
	for index, prefix := range queryNamePrefixes {
		if strings.HasPrefix(query, prefix) {
			return queryNamePrefixNames[index]
		}
	}
	return ""
}
