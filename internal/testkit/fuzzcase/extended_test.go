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

package fuzzcase

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtendedModelKeepsStableOperationsAndBindingBounds(t *testing.T) {
	require.True(t, extendedVersion == 3 && shapeVariableExpansion == 24 && shapeBranchClauses == 25 && shapeBulkInsert == 26 && shapeLocking == 27,
		"changed an assigned extended operation")
	for engine := range capabilities {
		for _, seed := range extendedSeeds(engine) {
			model, ok := Decode(engine, seed)
			require.True(t, ok, "rejected extended seed %s %v", engine, seed)
			query := model.Query()
			require.False(t, len(query.Values) > 8 || len(query.Occurrences) > 8, "extended model exceeded the binding bound")
			require.True(t, reflect.DeepEqual(query, model.Query()), "extended model changed across calls")
			for width := 1; width < len(seed); width++ {
				shorter, accepted := Decode(engine, seed[:width])
				require.False(t, accepted && (shorter.Rows > model.Rows || len(shorter.Query().Occurrences) > 8),
					"shortening grew the data or exceeded the binding bound")
			}
		}
	}
}

func TestExtendedCountReferencesKeepPaginationAndRemoveOuterLocks(t *testing.T) {
	compound, ok := Decode("postgres", []byte{3, 1, 1, 8, 3, 7, 1, 1, 2})
	require.True(t, ok, "rejected compound model")
	reference := compound.Query().CountReference
	for _, required := range []string{"ORDER BY id LIMIT 3", "ORDER BY id DESC LIMIT 3 OFFSET 1", "UNION ALL", "ORDER BY 1 LIMIT 2 OFFSET 1"} {
		require.Contains(t, reference, required, "count reference lost a branch clause")
	}
	locked, ok := Decode("postgres", []byte{3, 3, 1, 8, 3, 7})
	require.True(t, ok, "rejected lock model")
	query := locked.Query()
	require.True(t, strings.HasSuffix(query.Reference, " FOR UPDATE"), "reference lost its lock: %+v", query)
	require.NotContains(t, query.CountReference, "FOR UPDATE", "count reference retained removable locking")
	require.Contains(t, query.CountReference, "LIMIT 1 OFFSET 0", "count reference lost the query's own LIMIT, whose rows Count counts")
	require.True(t, locked.CountWrapped(), "a query with its own LIMIT is counted in a subquery")
}
