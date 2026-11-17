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

const (
	// seedMigrations is the migration byte of a five-migration seed.
	seedMigrations = 4

	// seedParent shapes the first table: three columns, a primary key and a check.
	seedParent = 0x32

	// seedChild shapes the second table: three columns, every key and a generated column.
	seedChild = 0xF6

	// seedDefaults gives the first table's columns defaults.
	seedDefaults = 0x0A

	// kindDropColumn is the kind byte of operationDropColumn.
	kindDropColumn = 2

	// kindPrimaryKey is the kind byte of operationPrimaryKey.
	kindPrimaryKey = 9

	// kindDropTable is the kind byte of operationDropTable.
	kindDropTable = 11

	// kindRenameTable is the kind byte of operationRenameTable.
	kindRenameTable = 12

	// kindNamespace is the kind byte of operationNamespace.
	kindNamespace = 18

	// seedKeyed gives a created table a primary key.
	seedKeyed = 1 << shapePrimaryKey

	// seedReferencing gives a created table a foreign key.
	seedReferencing = 1 << shapeForeignKey

	// seedChildFamily picks the second table's first family.
	seedChildFamily = 4

	// seedFamilyFlags gives every column of a family seed a default and NULL.
	seedFamilyFlags = 0x3F

	// seedMixed mixes nullability and defaults in the second table.
	seedMixed = 0x15

	// seedUnnamed leaves a created table's constraints for the engine to name.
	seedUnnamed = 1 << flagUnnamed

	// seedColumnLevel declares them with their columns.
	seedColumnLevel = 1 << flagColumnLevel

	// kindCheckOperation is the kind byte of operationCheck.
	kindCheckOperation = 7

	// kindUniqueOperation is the kind byte of operationUnique.
	kindUniqueOperation = 8

	// kindForeignKey is the kind byte of operationForeignKey.
	kindForeignKey = 10

	// seedPrimaryIndex drops a MySQL primary key as the index PRIMARY.
	seedPrimaryIndex = 2

	// seedSixMigrations is the migration byte of a six-migration seed.
	seedSixMigrations = 5

	// seedThreeMigrations is the migration byte of a three-migration seed.
	seedThreeMigrations = 2
)

// seedVariations lists the flag and variant bytes each operation's seeds use.
var seedVariations = [][2]byte{{0x00, 0}, {0x0F, 3}, {0xF0, 7}, {0xFF, 11}}

// Seeds returns inputs that reach every operation and type family the engine supports.
//
// Takes engine (string) which names the dialect.
//
// Returns [][]byte which holds the seeds, or nil for an unmodelled engine.
func Seeds(engine string) [][]byte {
	if !Supported(engine) {
		return nil
	}
	parent := []byte{0, 0, seedParent, 0, seedDefaults, 0}
	child := []byte{0, 1, seedChild, seedChildFamily, seedMixed, 1}
	var result [][]byte
	for kind := range len(operations) {
		for index, variation := range seedVariations {
			flags, variant := variation[0], variation[1]
			seed := []byte{SchemaHistoryVersion, seedMigrations}
			for _, operation := range [][]byte{parent, child,
				{byte(kind), byte(index), byte(index), variant, flags, byte(index)},
				{byte(kind), byte(index + 1), byte(index + 1), variant + 1, flags ^ 1, byte(index)},
				{byte(kind), byte(index + 2), byte(index), variant + 2, flags ^ 2, byte(index)}} {
				seed = append(seed, 0)
				seed = append(seed, operation...)
			}
			result = append(result, seed)
		}
	}

	for _, kind := range []byte{kindDropColumn, kindPrimaryKey, kindDropTable, kindRenameTable} {
		result = append(result, []byte{SchemaHistoryVersion, 2, 0, 0, 0, 1, 0, 0, 0,
			0, kind, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	}

	for _, level := range []byte{0, seedColumnLevel} {
		parent := []byte{0, 0, 0, seedParent, 0, seedUnnamed | level | seedDefaults, 0}
		child := []byte{0, 0, 1, seedChild, seedChildFamily, seedUnnamed | level | seedMixed, 1}
		seed := append([]byte{SchemaHistoryVersion, seedSixMigrations}, parent...)
		seed = append(seed, child...)
		for _, kind := range []byte{kindRenameTable, kindUniqueOperation, kindCheckOperation, kindForeignKey} {
			seed = append(seed, 0, kind, 1, 0, 0, 1, 0)
		}
		result = append(result, seed)
		seed = append([]byte{SchemaHistoryVersion, seedThreeMigrations}, parent...)
		seed = append(seed, child...)
		result = append(result, append(seed, 0, kindPrimaryKey, 1, 0, 0, seedPrimaryIndex, 0))
	}

	result = append(result, []byte{SchemaHistoryVersion, 1, 0, kindNamespace, 0, seedKeyed, 0, 0, 0, 0, 0, 0, seedReferencing})
	for variant := range len(allFamilies) {
		result = append(result, []byte{SchemaHistoryVersion, 1, 0, 0, 0, 2, byte(variant), seedFamilyFlags, 0,
			0, 1, 0, 0, byte(variant), 1, 0})
	}
	return result
}
