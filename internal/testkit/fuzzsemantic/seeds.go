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

package fuzzsemantic

import (
	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
)

// seedTables contains row sets combined with every predicate.
//
// The sets cover grouped items, repeated and NULL values, dangling references, smaller
// NULL cases, and empty tables.
var seedTables = [][]byte{
	{0x88, 6, 5, 7, 1, 7, 3, 11, 0, 6, 3, 3, 2, 13, 5, 5, 18, 4, 7, 17, 7, 7, 7, 2, 4,
		1, 5, 1, 1, 0, 6, 3, 7, 9, 3, 5, 5, 8, 2, 2, 4},
	{0x23, 0, 7, 7, 4, 7, 1, 2, 7, 7, 0, 7, 0, 2},
	{0x00},
}

// Seeds returns positive inputs for every predicate, emitter mode and row set.
//
// Takes engine (string) which names the dialect.
//
// Returns [][]byte which reaches every relation family the engine admits.
func Seeds(engine string) [][]byte {
	if !Supported(engine) {
		return nil
	}
	var seeds [][]byte
	for _, mode := range []fuzzbuilder.Mode{fuzzbuilder.ModeDatabaseSQL, fuzzbuilder.ModePrepared, fuzzbuilder.ModeNative} {
		if !fuzzbuilder.ModeSupported(engine, mode) {
			continue
		}
		for table, rows := range seedTables {
			for index := range predicates {
				if table > 0 && index%len(seedTables) != table {
					continue
				}
				choice := byte(index)
				header := []byte{SemanticVersion, byte(mode), choice, choice * 2 % parameterChoices, choice, choice % (pageChoices * 2)}
				seeds = append(seeds, append(header, rows...))
			}
		}
	}
	return seeds
}
