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
	"strings"

	"seagull.sh/seagull/sdk/dialect"
)

const (
	// fuzzTextTypeName is the explicit text metadata spelling.
	fuzzTextTypeName = "text"

	// fuzzBlobTypeName is the explicit binary storage spelling.
	fuzzBlobTypeName = "blob"
)

// fuzzScalarTypes maps each engine's text and binary type names to their categories.
var fuzzScalarTypes = map[string]map[string]dialect.SQLTypeCategory{
	enginePostgres:    {fuzzTextTypeName: dialect.TypeCategoryText, "bytea": dialect.TypeCategoryBinary},
	engineCockroachDB: {fuzzTextTypeName: dialect.TypeCategoryText, "string": dialect.TypeCategoryText, "bytea": dialect.TypeCategoryBinary, "bytes": dialect.TypeCategoryBinary},
	engineTimescaleDB: {fuzzTextTypeName: dialect.TypeCategoryText, "bytea": dialect.TypeCategoryBinary},
	engineSQLite:      {fuzzTextTypeName: dialect.TypeCategoryText, fuzzBlobTypeName: dialect.TypeCategoryBinary},
	engineDuckDB:      {"varchar": dialect.TypeCategoryText, fuzzBlobTypeName: dialect.TypeCategoryBinary},
	engineMySQL:       {fuzzTextTypeName: dialect.TypeCategoryText, "varbinary": dialect.TypeCategoryBinary, "binary": dialect.TypeCategoryBinary, fuzzBlobTypeName: dialect.TypeCategoryBinary},
	engineMariaDB:     {fuzzTextTypeName: dialect.TypeCategoryText, "varbinary": dialect.TypeCategoryBinary, "binary": dialect.TypeCategoryBinary, fuzzBlobTypeName: dialect.TypeCategoryBinary},
	engineClickHouse:  {"string": dialect.TypeCategoryText},
}

// fuzzScalarType recognises the explicit text and binary types used by value models.
//
// Takes engine (string) which selects native binding and metadata rules.
// Takes name (string) which contains the native metadata spelling.
//
// Returns ReportedType which supplements the existing numeric adapters.
// Returns bool which rejects unknown names instead of guessing a category.
func fuzzScalarType(engine, name string) (ReportedType, bool) {
	normal := strings.ToLower(strings.TrimSpace(name))
	if engine == engineClickHouse && strings.HasPrefix(normal, "nullable(") {
		normal = strings.TrimSuffix(strings.TrimPrefix(normal, "nullable("), ")")
	}
	category, ok := fuzzScalarTypes[engine][normal]
	if !ok {
		category, ok = fuzzExtendedScalarType(engine, normal)
	}
	return ReportedType{Category: category, Width: 0, Unsigned: false}, ok
}

// fuzzExtendedScalarType recognises admitted metadata outside basic text and binary
// names.
//
// Takes engine (string) which selects native binding and metadata rules.
// Takes normal (string) which contains the normalised native type name.
//
// Returns dialect.SQLTypeCategory which identifies the admitted native family.
// Returns bool which rejects unknown metadata spellings.
func fuzzExtendedScalarType(engine, normal string) (dialect.SQLTypeCategory, bool) {
	if isPostgresFamily(engine) {
		switch normal {
		case "varchar", "character varying", "char", "character", "bpchar", "name":
			return dialect.TypeCategoryText, true
		case "_int8":
			return dialect.TypeCategoryArray, true
		}
	}
	if engine == engineMySQL || engine == engineMariaDB {
		switch normal {
		case "char", "varchar", "tinytext", "mediumtext", "longtext":
			return dialect.TypeCategoryText, true
		case "tinyblob", "mediumblob", "longblob":
			return dialect.TypeCategoryBinary, true
		}
	}
	if engine == engineSQLite {
		return dialect.TypeCategoryUnknown, false
	}
	return fuzzCommonScalarType(normal)
}

// fuzzCommonScalarType recognises explicitly admitted shared scalar and collection names.
//
// Takes normal (string) which contains the normalised native type name.
//
// Returns dialect.SQLTypeCategory which identifies the admitted native family.
// Returns bool which rejects unknown metadata spellings.
func fuzzCommonScalarType(normal string) (dialect.SQLTypeCategory, bool) {
	var category dialect.SQLTypeCategory
	ok := false
	base, _, _ := strings.Cut(normal, "(")
	switch base {
	case "bool", "boolean":
		category, ok = dialect.TypeCategoryBoolean, true
	case "date", "date32", "datetime", "datetime64", "timestamp", "timestamp with time zone", "timestamp without time zone", "timestamptz", "interval":
		category, ok = dialect.TypeCategoryTemporal, true
	case "uuid":
		category, ok = dialect.TypeCategoryUUID, true
	case "json", "jsonb":
		category, ok = dialect.TypeCategoryJSON, true
	case "array":
		category, ok = dialect.TypeCategoryArray, true
	}
	if strings.HasSuffix(normal, "[]") {
		category, ok = dialect.TypeCategoryArray, true
	}
	return category, ok
}
