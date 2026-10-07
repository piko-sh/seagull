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

package dialect_test

import (
	"testing"

	"seagull.sh/seagull/internal/apitest"
	"seagull.sh/seagull/sdk/dialect"
)

func TestDialectFacadeAPI(t *testing.T) {
	t.Parallel()

	surface := apitest.Surface{
		"Dialect": dialect.Dialect{},
		"Facts":   dialect.Facts{},

		"Placeholders":     dialect.Placeholders{},
		"PlaceholderSigil": dialect.PlaceholderSigil{},

		"PlaceholderKind": dialect.PlaceholderKind(0),

		"PlaceholderNumbered":  dialect.PlaceholderNumbered,
		"PlaceholderAnonymous": dialect.PlaceholderAnonymous,
		"PlaceholderNamed":     dialect.PlaceholderNamed,

		"DataChange": dialect.DataChange(0),

		"DataChangeInsert": dialect.DataChangeInsert,
		"DataChangeUpdate": dialect.DataChangeUpdate,
		"DataChangeDelete": dialect.DataChangeDelete,
		"DataChangeMerge":  dialect.DataChangeMerge,

		"Catalogue":       dialect.Catalogue{},
		"Schema":          dialect.Schema{},
		"Table":           dialect.Table{},
		"Column":          dialect.Column{},
		"TableName":       dialect.TableName{},
		"MigrationOrigin": dialect.MigrationOrigin{},
		"SQLType":         dialect.SQLType{},

		"SQLTypeCategory": dialect.SQLTypeCategory(0),

		"TypeCategoryInteger":        dialect.TypeCategoryInteger,
		"TypeCategoryFloat":          dialect.TypeCategoryFloat,
		"TypeCategoryDecimal":        dialect.TypeCategoryDecimal,
		"TypeCategoryBoolean":        dialect.TypeCategoryBoolean,
		"TypeCategoryText":           dialect.TypeCategoryText,
		"TypeCategoryBinary":         dialect.TypeCategoryBinary,
		"TypeCategoryTemporal":       dialect.TypeCategoryTemporal,
		"TypeCategoryJSON":           dialect.TypeCategoryJSON,
		"TypeCategoryUUID":           dialect.TypeCategoryUUID,
		"TypeCategoryNetwork":        dialect.TypeCategoryNetwork,
		"TypeCategoryGeometric":      dialect.TypeCategoryGeometric,
		"TypeCategoryEnum":           dialect.TypeCategoryEnum,
		"TypeCategoryComposite":      dialect.TypeCategoryComposite,
		"TypeCategoryArray":          dialect.TypeCategoryArray,
		"TypeCategoryRange":          dialect.TypeCategoryRange,
		"TypeCategoryStruct":         dialect.TypeCategoryStruct,
		"TypeCategoryMap":            dialect.TypeCategoryMap,
		"TypeCategoryUnion":          dialect.TypeCategoryUnion,
		"TypeCategoryUnknown":        dialect.TypeCategoryUnknown,
		"TypeCategoryAggregateState": dialect.TypeCategoryAggregateState,

		"MaxTypeDepth": dialect.MaxTypeDepth,
		"NewSQLType":   dialect.NewSQLType,
		"NewColumn":    dialect.NewColumn,
	}

	apitest.Check(t, surface, "facade_test.golden.yaml")
}
