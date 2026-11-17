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
package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAliasWithoutAsAfterATableNamedFrom(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		alias   string
		refused bool
	}{
		{name: "a quoted table named from refuses an alias without AS", sql: `SELECT t.id FROM "from" t`, refused: true},
		{name: "a backticked table named FROM refuses one too", sql: "SELECT t.id FROM `FROM` t", refused: true},
		{name: "a joined table named from refuses one", sql: `SELECT t.id FROM other o JOIN "from" t ON o.id = t.id`, refused: true},
		{name: "AS gives a table named from its alias", sql: `SELECT t.id FROM "from" AS t`, alias: "t"},
		{name: "a schema-qualified table named from takes an alias without AS", sql: `SELECT t.id FROM db."from" t`, alias: "t"},
		{name: "a table named from with no alias is read", sql: `SELECT id FROM "from" WHERE id = 1`},
		{name: "another quoted keyword takes an alias without AS", sql: `SELECT t.id FROM "select" t`, alias: "t"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.refused {
				assert.ErrorContains(t, analyseError(t, testCase.sql), "alias after a table named from only with AS")
				return
			}
			analysis := analyse(t, testCase.sql)
			require.NotEmpty(t, analysis.FromTables)
			assert.Equal(t, testCase.alias, analysis.FromTables[len(analysis.FromTables)-1].Alias)
		})
	}
}
