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

package chvalues

import (
	"strings"

	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// DocumentsFile is the generated file holding the reading of the JSON documents
	// clickhouse-go decodes, which both ClickHouse emitters' scan adapters call.
	DocumentsFile = "clickhouse_documents.go"

	// documentsSourceName is the name of the reading's source file.
	documentsSourceName = "source/clickhouse_documents.go"

	// DocumentPackage is the import path of the clickhouse-go package declaring the type it
	// decodes a JSON value into, chcol.JSON.
	DocumentPackage = "github.com/ClickHouse/clickhouse-go/v2/lib/chcol"

	// documentType is chcol.JSON as generated code writes it.
	documentType = "chcol.JSON"

	// typeModifiers are the characters a GoType's Name starts with to make a pointer to or a
	// slice of the type after them.
	typeModifiers = "*[]"
)

// DocumentHelpers lists the helper-prefix suffixes of the identifiers the reading's file
// declares, so an emitter can reserve them.
var DocumentHelpers = []string{
	"ClickHouseDocumentDepth", "ReadClickHouseDocuments", "ClickHouseDocumentText", "ReadClickHouseText",
	"ReadClickHouseLooseText", "ClickHouseMayHold", "ClickHouseLooseType", "ClickHouseLoose", "ClickHouseLooseInterface",
	"ClickHouseLooseList", "ClickHouseLooseMap", "ClickHouseLooseDocument",
}

// Documents renders the shared reader for driver-decoded ClickHouse JSON documents.
//
// Both emitters use the same reader, preserving the same JSON bytes for stored documents.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
// Takes jsonType (helpers.Code) which writes the package's JSON type, as JSONType gives
// it.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be read or rendered.
func Documents(vocabulary helpers.Vocabulary, packageName string, jsonType helpers.Code) (sqlmodel.GeneratedFile, error) {
	return renderData(DocumentsFile, documentsSourceName, vocabulary, &helpers.Data{
		Flags: map[string]bool{"standIns": false},
		Values: map[string]any{
			"documentType": DocumentType(),
			"jsonType":     jsonType,
		},
		Renames: nil,
		Package: packageName,
	})
}

// DocumentType returns the type clickhouse-go decodes a JSON value into, chcol.JSON, as a
// helper file writes it, with its import.
//
// Returns helpers.Code which writes the type.
func DocumentType() helpers.Code {
	return helpers.Code{Text: documentType, Imports: []string{DocumentPackage}}
}

// JSONType returns the Go type the request's JSON slot maps a document to, as a helper
// file writes it: the package's own JSON type by its name, and a type of another package
// qualified by the name its import path implies, with the import.
//
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns helpers.Code which writes the type.
func JSONType(types sqlmodel.GoTypeResolver) helpers.Code {
	var document sqlmodel.SQLType
	document.Category = sqlmodel.TypeCategoryJSON
	resolved := goast.ResolveGoType(document, false, types)
	if resolved.Package == "" {
		return helpers.Code{Text: resolved.Name, Imports: nil}
	}
	name := strings.TrimLeft(resolved.Name, typeModifiers)
	modifiers := resolved.Name[:len(resolved.Name)-len(name)]
	return helpers.Code{Text: modifiers + goast.AssumedPackageName(resolved.Package) + "." + name, Imports: []string{resolved.Package}}
}

// HoldsJSON reports whether a type is a JSON document or holds one at any depth, which
// clickhouse-go hands a scan adapter decoded into its own chcol.JSON.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
//
// Returns bool which is true when the SQL type contains a document.
func HoldsJSON(sqlType *sqlmodel.SQLType) bool {
	return holdsJSON(sqlType, 0)
}

// MapsJSON reports Maps whose non-null JSON values require entry-by-entry reading.
//
// The driver's OrderedMap can read entries individually; ordinary Go maps panic while the
// row is built (clickhouse-go v2.48).
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
//
// Returns bool which is true for such a Map.
func MapsJSON(sqlType *sqlmodel.SQLType) bool {
	return sqlType != nil && sqlType.Category == sqlmodel.TypeCategoryMap && sqlType.ElementType != nil &&
		sqlType.ElementType.Category == sqlmodel.TypeCategoryJSON && !sqlType.ElementType.Nullable
}

// NestsJSONMap reports whether a type holds a Map MapsJSON reports inside an Array, a Map
// or a Tuple, which clickhouse-go builds as it builds the row, so it panics reading the
// column whatever the destination.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
//
// Returns bool which is true when the type nests such a Map.
func NestsJSONMap(sqlType *sqlmodel.SQLType) bool {
	if sqlType == nil {
		return false
	}
	for _, part := range typeParts(sqlType) {
		if holdsJSONMap(part, 0) {
			return true
		}
	}
	return false
}

// holdsJSON reports whether a type is or holds a JSON document, within a bounded depth.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
// Takes depth (int) which is how deep the type is nested.
//
// Returns bool which is true when it holds one.
func holdsJSON(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	if sqlType.Category == sqlmodel.TypeCategoryJSON {
		return true
	}
	for _, part := range typeParts(sqlType) {
		if holdsJSON(part, depth+1) {
			return true
		}
	}
	return false
}

// holdsJSONMap reports whether a type is or holds a Map MapsJSON reports, within a
// bounded depth.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
// Takes depth (int) which is how deep the type is nested.
//
// Returns bool which is true when it holds one.
func holdsJSONMap(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	if MapsJSON(sqlType) {
		return true
	}
	for _, part := range typeParts(sqlType) {
		if holdsJSONMap(part, depth+1) {
			return true
		}
	}
	return false
}

// typeParts returns the types a type holds directly: an Array's element, a Map's key and
// value, and a Tuple's elements.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns []*sqlmodel.SQLType which holds the parts.
func typeParts(sqlType *sqlmodel.SQLType) []*sqlmodel.SQLType {
	switch sqlType.Category {
	case sqlmodel.TypeCategoryArray:
		return []*sqlmodel.SQLType{sqlType.ElementType}
	case sqlmodel.TypeCategoryMap:
		return []*sqlmodel.SQLType{sqlType.KeyType, sqlType.ElementType}
	case sqlmodel.TypeCategoryStruct:
		parts := make([]*sqlmodel.SQLType, len(sqlType.StructFields))
		for index := range sqlType.StructFields {
			parts[index] = &sqlType.StructFields[index].SQLType
		}
		return parts
	default:
		return nil
	}
}
