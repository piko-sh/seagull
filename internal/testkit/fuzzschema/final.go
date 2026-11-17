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

package fuzzschema

import (
	"slices"
	"strings"
)

// cockroachRowID is the hidden key column CockroachDB adds to a table without a key.
const cockroachRowID = "rowid"

// final exports the schema the history leaves.
//
// Returns Schema which lists every live object.
func (state *history) final() Schema {
	schema := Schema{Enums: make(map[string][]string, len(state.enums)), Relations: nil, Sequences: sortedCopy(state.sequences)}
	for _, created := range state.enums {
		schema.Enums[created.name] = slices.Clone(created.values)
	}
	for _, table := range state.relations {
		exported := table.export("")
		if state.engine == engineCockroachDB && table.kind != KindView && len(exported.PrimaryKey) == 0 {

			exported.PrimaryKey = []string{cockroachRowID}
		}
		schema.Relations = append(schema.Relations, exported)
		for _, index := range table.partitions {
			schema.Relations = append(schema.Relations, table.partition(index).export(table.key()))
		}
	}
	slices.SortFunc(schema.Relations, func(left, right Relation) int { return strings.Compare(left.Key(), right.Key()) })
	return schema
}

// export copies a relation's model into its public form.
//
// Takes parent (string) which is the key of the partitioned table it belongs to, or
// empty.
//
// Returns Relation which owns its slices.
func (table *relation) export(parent string) Relation {
	result := Relation{Namespace: table.namespace, Name: table.name, Kind: table.kind, PartitionOf: parent, Columns: nil,
		PrimaryKey: slices.Clone(table.primaryKey), Uniques: nil, Checks: sortedCopy(table.checks), ForeignKeys: nil}
	for _, member := range table.columns {
		result.Columns = append(result.Columns, Column{Name: member.name, Family: member.family, Nullable: member.nullable,
			Default: member.hasDefault, Generated: member.generated})
	}
	for _, key := range table.uniques {
		result.Uniques = append(result.Uniques, slices.Clone(key.columns))
	}
	slices.SortFunc(result.Uniques, func(left, right []string) int { return strings.Compare(list(left), list(right)) })
	for _, key := range table.foreign {
		result.ForeignKeys = append(result.ForeignKeys, ForeignKey{Table: key.table, Columns: slices.Clone(key.columns),
			References: slices.Clone(key.references)})
	}
	return result
}
