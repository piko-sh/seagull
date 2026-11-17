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

func TestMigrationOracleRejectsOmittedOrChangedOperations(t *testing.T) {
	baseline := func() map[string]fuzzSchemaRelation {
		return map[string]fuzzSchemaRelation{
			"fuzz_items": {
				columns: []fuzzSchemaColumn{{name: "id"}, {name: "score", nullable: true}, {name: "added", nullable: true}},
				indexes: map[string]fuzzSchemaIndex{"fuzz_model_index": {columns: []string{"score", "id"}, unique: true}},
			},
			"fuzz_view": {view: true, columns: []fuzzSchemaColumn{{name: "id"}}, indexes: map[string]fuzzSchemaIndex{}},
		}
	}
	tests := map[string]func(map[string]fuzzSchemaRelation){
		"missing create": func(relations map[string]fuzzSchemaRelation) { delete(relations, "fuzz_items") },
		"missing drop":   func(relations map[string]fuzzSchemaRelation) { relations["fuzz_stale"] = fuzzSchemaRelation{} },
		"missing rename": func(relations map[string]fuzzSchemaRelation) {
			relations["fuzz_old"] = relations["fuzz_items"]
			delete(relations, "fuzz_items")
		},
		"missing added column": func(relations map[string]fuzzSchemaRelation) {
			table := relations["fuzz_items"]
			table.columns = table.columns[:2]
			relations["fuzz_items"] = table
		},
		"missing column rename": func(relations map[string]fuzzSchemaRelation) { relations["fuzz_items"].columns[2].name = "previous" },
		"changed column order": func(relations map[string]fuzzSchemaRelation) {
			columns := relations["fuzz_items"].columns
			columns[0], columns[1] = columns[1], columns[0]
		},
		"lost not null": func(relations map[string]fuzzSchemaRelation) { relations["fuzz_items"].columns[0].nullable = true },
		"missing index": func(relations map[string]fuzzSchemaRelation) {
			delete(relations["fuzz_items"].indexes, "fuzz_model_index")
		},
		"wrong index order": func(relations map[string]fuzzSchemaRelation) {
			relations["fuzz_items"].indexes["fuzz_model_index"] = fuzzSchemaIndex{columns: []string{"id", "score"}, unique: true}
		},
		"lost uniqueness": func(relations map[string]fuzzSchemaRelation) {
			relations["fuzz_items"].indexes["fuzz_model_index"] = fuzzSchemaIndex{columns: []string{"score", "id"}}
		},
		"wrong relation kind": func(relations map[string]fuzzSchemaRelation) {
			view := relations["fuzz_view"]
			view.view = false
			relations["fuzz_view"] = view
		},
	}
	require.Empty(t, fuzzSchemaProblems(baseline(), baseline()))
	for name, damage := range tests {
		t.Run(name, func(t *testing.T) {
			broken := baseline()
			damage(broken)
			require.NotEmpty(t, fuzzSchemaProblems(broken, baseline()))
			require.NotEmpty(t, fuzzSchemaProblems(baseline(), broken))
		})
	}
}

func TestMigrationOracleDoesNotInferViewConstraintsFromMetadata(t *testing.T) {
	replayed := map[string]fuzzSchemaRelation{"fuzz_view": {view: true, columns: []fuzzSchemaColumn{{name: "id"}}}}
	observed := map[string]fuzzSchemaRelation{"fuzz_view": {view: true, columns: []fuzzSchemaColumn{{name: "id", nullable: true}}}}
	require.Empty(t, fuzzSchemaProblems(replayed, observed))
	view := observed["fuzz_view"]
	view.columns[0].name = "wrong"
	require.NotEmpty(t, fuzzSchemaProblems(replayed, observed))
}

func TestDuckIndexMetadataRetainsKeyOrderAndRejectsUnexpectedGrammar(t *testing.T) {
	columns, err := fuzzDuckIndexColumns(`CREATE INDEX fuzz_model_index ON fuzz_items ("category", id);`)
	require.NoError(t, err)
	require.Equal(t, []string{"category", "id"}, columns)
	for _, invalid := range []string{"", "CREATE INDEX x ON y (id", "CREATE INDEX x ON y ()", "CREATE INDEX x ON y (id, score)", "CREATE INDEX x ON y (lower(category), id)", "CREATE INDEX x ON y (id) WHERE id > 0"} {
		_, err := fuzzDuckIndexColumns(invalid)
		require.Error(t, err)
	}
}
