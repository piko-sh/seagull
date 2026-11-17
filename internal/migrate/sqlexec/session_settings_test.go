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

package sqlexec

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

func TestSessionStatementsMarksForeignKeyPragmas(t *testing.T) {
	t.Parallel()

	statements := []string{
		"PRAGMA foreign_keys = OFF",
		"CREATE TABLE t (id INTEGER)",
		"pragma FOREIGN_KEYS = ON",
	}
	require.Equal(t, map[int]bool{0: true, 2: true}, sqliteEngine().sessionStatements(statements))
	require.Nil(t, postgresEngine().sessionStatements(statements), "an engine without such settings marks none")
	require.Nil(t, sqliteEngine().sessionStatements([]string{"SELECT 1"}))
}

func TestSettingsAroundTransaction(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		session map[int]bool
		name    string
		before  []int
		after   []int
		count   int
		refused bool
	}{
		{name: "no session statements", count: 2, session: nil},
		{name: "opening and closing statements", count: 5, session: map[int]bool{0: true, 1: true, 3: true, 4: true}, before: []int{0, 1}, after: []int{3, 4}},
		{name: "every statement", count: 2, session: map[int]bool{0: true, 1: true}, before: []int{0, 1}},
		{name: "a statement between others", count: 3, session: map[int]bool{1: true}, refused: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			run := migrationRun{
				session:    testCase.session,
				statements: make([]string, testCase.count),
				record:     migrate.MigrationRecord{Version: 7, Name: "rebuild"},
			}
			before, after, err := settingsAroundTransaction(&run)
			if testCase.refused {
				require.ErrorIs(t, err, migrate.ErrSettingInsideTransaction)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.before, before)
			require.Equal(t, testCase.after, after)
		})
	}
}
