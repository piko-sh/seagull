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

package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHistoryTextHelpersReadEveryDriverSpelling(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"1", "t", "TRUE", "yes"} {
		require.True(t, historyBool(value), value)
	}
	require.False(t, historyBool("0"))
	require.Nil(t, historySplit("  "))
	require.Equal(t, []string{"a", "b c"}, historySplit(` a ,"b c"`))
	require.Equal(t, []string{"fuzz_ck_1"}, historyChecks("CREATE TABLE t (x INT, CONSTRAINT \"fuzz_ck_1\" CHECK (x > 0))"))
}

func TestMySQLAndSQLiteFamiliesFallBackToTheEngineType(t *testing.T) {
	t.Parallel()
	require.Equal(t, "boolean", mysqlHistoryFamily("tinyint", "tinyint(1)"))
	require.Equal(t, "other:geometry", mysqlHistoryFamily("geometry", "geometry"))
	require.Equal(t, "integer", sqliteHistoryFamily("integer"))
	require.Equal(t, "other:POINT", sqliteHistoryFamily("POINT"))
}
