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

package assemble

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
)

func TestSQLTypeNameNamesEachLevel(t *testing.T) {
	t.Parallel()

	deep := dialect.SQLType{EngineName: "integer"}
	for range dialect.MaxTypeDepth + 5 {
		deep = dialect.SQLType{ElementType: new(deep)}
	}

	testCases := []struct {
		name     string
		sqlType  dialect.SQLType
		expected string
	}{
		{name: "an engine name", sqlType: dialect.SQLType{EngineName: "text"}, expected: "text"},
		{name: "an array the engine does not name", sqlType: dialect.SQLType{ElementType: &dialect.SQLType{EngineName: "integer"}}, expected: "integer[]"},
		{name: "no name at all", sqlType: dialect.SQLType{}, expected: "unknown"},
		{name: "an unnamed array of unnamed elements", sqlType: dialect.SQLType{ElementType: &dialect.SQLType{}}, expected: "unknown[]"},
		{name: "nesting past the deepest type", sqlType: deep, expected: "unknown" + strings.Repeat("[]", dialect.MaxTypeDepth)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, SQLTypeName(testCase.sqlType))
		})
	}
}
