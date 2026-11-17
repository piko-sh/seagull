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

	"seagull.sh/seagull/internal/testkit/fuzzschema"
	"seagull.sh/seagull/sdk/dialect"
)

func TestCompareHistoryBlamesTheRightSide(t *testing.T) {
	t.Parallel()
	model := map[string]string{"t columns": "a,b", "v" + historyMaterialised: historyTrue}
	live := map[string]string{"t columns": "a,b", "v" + historyMaterialised: historyTrue}
	require.Empty(t, compareHistory(model, live, map[string]string{"t columns": "a,b"}), "Seagull keeps materialised views with views")
	problems := compareHistory(model, live, map[string]string{"t columns": "a"})
	require.Len(t, problems, 1)
	require.Contains(t, problems[0], "[seagull]")
	problems = compareHistory(model, map[string]string{"t columns": "b,a", "v" + historyMaterialised: historyTrue}, model)
	require.Len(t, problems, 1)
	require.Contains(t, problems[0], "[model]")
}

func TestSeagullHistoryFamilyMapsEveryCategory(t *testing.T) {
	t.Parallel()
	require.Equal(t, fuzzschema.FamilyArray, seagullHistoryFamily(dialect.SQLType{Category: dialect.TypeCategoryInteger}, 1))
	require.Equal(t, fuzzschema.FamilyInteger, seagullHistoryFamily(dialect.SQLType{Category: dialect.TypeCategoryInteger}, 0))
	require.Equal(t, fuzzschema.FamilyTimestampTZ, seagullHistoryFamily(dialect.SQLType{Category: dialect.TypeCategoryTemporal,
		Temporal: dialect.TemporalKindTimestamp, TimeZone: true}, 0))
	require.Equal(t, "other:point", seagullHistoryFamily(dialect.SQLType{Category: dialect.TypeCategoryUnknown, EngineName: "point"}, 0))
}

func TestHistoryKeysLeaveOutThePrimaryKeyAndRepeats(t *testing.T) {
	t.Parallel()
	require.Equal(t, "a|b,c", historyKeys([][]string{{"id"}, {"b", "c"}, {"a"}, {"a"}, nil}, []string{"id"}))
}
