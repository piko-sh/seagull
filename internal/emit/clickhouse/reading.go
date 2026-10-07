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

package clickhouse

import (
	"embed"
	"fmt"
	"go/ast"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// compositesFile is the generated file holding the converting scan adapter.
	compositesFile = "clickhouse_composites.go"

	// compositesSource is the path of the adapter's source among the embedded sources.
	compositesSource = "internal/source/native_composites.go"

	// suffixScanNativeComposite is the helper-prefix suffix of the adapter's constructor.
	suffixScanNativeComposite = "ScanNativeComposite"

	// suffixReadNativeColumn is the helper-prefix suffix of the function converting the map
	// the driver fills for a named tuple column.
	suffixReadNativeColumn = "ReadNativeColumn"

	// suffixNativeEntries is the helper-prefix suffix of the type a Map column whose values
	// are JSON documents is scanned into, entry by entry.
	suffixNativeEntries = "NativeEntries"

	// bitsInLong is the width of the widest integer the native driver reads as a Go integer
	// rather than a big.Int.
	bitsInLong = 64
)

const (
	// placeColumn is the column itself, which the driver reads into the destination's own Go
	// type for every value but a named tuple.
	placeColumn valuePlace = iota

	// placeElement is an element of an Array, or a key or value of a Map, which the driver
	// builds in its own Go types.
	placeElement

	// placeField is an element of a named tuple, which the driver reads into a field of a
	// struct by converting its own value.
	placeField
)

var (
	// compositeHelpers lists the helper-prefix suffixes of the identifiers the adapter's
	// file declares, so the emitter can reserve them.
	compositeHelpers = []string{
		suffixScanNativeComposite, suffixReadNativeColumn, "NativeCompositeScanner", "ReadNativeValue", "ReadNativeShape", "ReadNativeAddress",
		"ReadNativeStruct", "NativeField", "NativeTagName", "ReadNativeText", "NativeText", "NativeNull",
	}

	// documentHelpers lists the helper-prefix suffixes of the identifiers the adapter's file
	// declares for a package reading JSON documents.
	documentHelpers = []string{"ReadNativeDocuments"}

	// entriesHelpers lists the helper-prefix suffixes of the identifiers the adapter's file
	// declares for a package reading a Map column whose values are JSON documents.
	entriesHelpers = []string{suffixNativeEntries, "ReadNativeEntries"}

	// compositeSources holds the adapter's source.
	//
	//go:embed internal/source/native_composites.go
	compositeSources embed.FS

	// preparedComposites caches the native composite scan helper template.
	preparedComposites = helpers.NewSources(compositeSources, "internal/source")
)

var (
	_ gogen.ColumnReader = (*nativeDriver)(nil)

	_ gogen.ColumnConverter = (*nativeDriver)(nil)

	_ gogen.RawConverter = (*nativeDriver)(nil)

	_ gogen.OverrideReader = (*nativeDriver)(nil)
)

// valuePlace is where a value sits in a column the native driver reads, which decides
// whether the driver hands it in a Go type of its own.
type valuePlace uint8

// compositeParts says which parts of the adapter's file a package uses besides the
// adapter itself.
type compositeParts struct {
	// documents includes the reading of JSON documents through clickhouse_documents.go, and
	// the adapter's DeserializeClickHouseJSON.
	documents bool

	// entries includes sglNativeEntries, which a Map column whose values are JSON documents
	// is scanned into.
	entries bool
}

// ColumnReading selects how native ClickHouse values reach generated Go types.
//
// Values needing conversion use a scan adapter, as do a JSON document and a value of
// unknown type, such as a Dynamic, which the driver scans into no interface value. When
// the driver bypasses sql.Scanner, the generated code scans into driver-native values and
// converts them afterwards.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns gogen.ColumnReading which says how the column is read.
func (*nativeDriver) ColumnReading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	switch {
	case sqlType.Category == sqlmodel.TypeCategoryJSON, sqlType.Category == sqlmodel.TypeCategoryUnknown:
		return gogen.ColumnReadAdapted
	case chvalues.MapsJSON(&sqlType), unnamedDocumentTuples(&sqlType) > 0:
		return gogen.ColumnReadRaw
	case !holdsDriverValues(&sqlType, placeColumn, 0):
		return gogen.ColumnReadAsIs
	default:
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryMap, sqlmodel.TypeCategoryArray:
		return gogen.ColumnReadConverted
	case sqlmodel.TypeCategoryStruct:
		return gogen.ColumnReadRaw
	default:
		return gogen.ColumnReadAsIs
	}
}

// RawType returns the native driver's Go type for a column read before conversion.
//
// Named tuples use maps keyed by element name; unnamed tuples use []any. Maps of
// documents use entry-based reading.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type, a Map of JSON
// documents or a tuple.
//
// Returns sqlmodel.GoType which is the raw local's Go type.
func (n *nativeDriver) RawType(sqlType sqlmodel.SQLType) sqlmodel.GoType {
	anything := sqlmodel.GoType{Composite: nil, Package: "", Name: "any"}
	switch {
	case chvalues.MapsJSON(&sqlType):
		return sqlmodel.GoType{Composite: nil, Package: "", Name: n.vocabulary.Helper(suffixNativeEntries)}
	case !isNamedTuple(&sqlType):
		return sqlmodel.GoType{Composite: nil, Package: "", Name: strings.Repeat("[]", unnamedDocumentTuples(&sqlType)) + "[]" + anything.Name}
	default:
		return sqlmodel.NewGoMap(sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}, anything)
	}
}

// ConvertRaw returns the call that converts the map the native driver filled for a named
// tuple column into the destination after the scan, as `sglReadNativeColumn(destination,
// raw)`, with the converting scan adapter's rules.
//
// Takes destination (ast.Expr) which is the address the column is read into.
// Takes raw (ast.Expr) which reads the map the driver filled.
//
// Returns ast.Expr which is the call, which returns an error.
func (n *nativeDriver) ConvertRaw(destination ast.Expr, raw ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(n.vocabulary.Helper(suffixReadNativeColumn)), destination, raw)
}

// ConvertingScan wraps a scan destination in the generated converting scan adapter, as
// `sglScanNativeComposite(destination)`, which converts the native driver's own value of
// the column into the destination's Go type.
//
// Takes destination (ast.Expr) which is the address the column is read into.
//
// Returns ast.Expr which is the scan argument.
func (n *nativeDriver) ConvertingScan(destination ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(n.vocabulary.Helper(suffixScanNativeComposite)), destination)
}

// ReadsOverride reports whether an overridden Go type uses the converting scan adapter.
//
// Tuple columns are read directly because the native driver fills their fields without
// calling sql.Scanner.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns bool which is true when the column is read through the adapter.
func (*nativeDriver) ReadsOverride(_ sqlmodel.GoType, sqlType sqlmodel.SQLType) bool {
	return sqlType.Category != sqlmodel.TypeCategoryStruct
}

// OverrideScan wraps a scan destination in the converting scan adapter, as
// `sglScanNativeComposite(destination)`, which converts the native driver's own value of
// the column into the override's Go type, whatever the column's SQL type.
//
// Takes destination (ast.Expr) which is the address the column is read into.
//
// Returns ast.Expr which is the scan argument.
func (n *nativeDriver) OverrideScan(destination ast.Expr, _ sqlmodel.SQLType) ast.Expr {
	return n.ConvertingScan(destination)
}

// renderComposites renders clickhouse_composites.go: the converting scan adapter, with
// the parts the package uses.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
// Takes parts (compositeParts) which says which parts the package uses.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be read or rendered.
func renderComposites(vocabulary helpers.Vocabulary, packageName string, parts compositeParts) (sqlmodel.GeneratedFile, error) {
	return preparedComposites.File(compositesFile, strings.TrimPrefix(compositesSource, "internal/source/"), vocabulary, &helpers.Data{
		Flags: map[string]bool{"documents": parts.documents, "entries": parts.entries, "standIns": false},
		Values: map[string]any{
			"readsDocuments": parts.documents,
			"readsEntries":   parts.entries,
			"documentType":   chvalues.DocumentType(),
		},
		Renames: nil,
		Package: packageName,
	})
}

// documentParts reports which JSON document reader helpers a package needs.
//
// Takes emission (*gogen.Emission) which is the package's generation.
//
// Returns compositeParts which says which parts the package uses.
func documentParts(emission *gogen.Emission) compositeParts {
	var parts compositeParts
	for _, query := range emission.Request.Queries {
		for index := range query.OutputColumns {
			column := &query.OutputColumns[index]
			if !chvalues.HoldsJSON(&column.SQLType) {
				continue
			}
			raw := emission.ReadsRaw(column)
			parts.documents = parts.documents || raw || emission.ConvertsColumn(column, false) || emission.ReadsOverride(column)
			parts.entries = parts.entries || raw && chvalues.MapsJSON(&column.SQLType)
		}
	}
	return parts
}

// unnamedDocumentTuples counts the Arrays around a tuple whose elements have no names and
// that holds a JSON document, as in Array(Tuple(JSON, Int64)), or returns zero for any
// other type, an unnamed tuple column included.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns int which is the number of Arrays around the tuple.
func unnamedDocumentTuples(sqlType *sqlmodel.SQLType) int {
	arrays := 0
	element := sqlType
	for element != nil && element.Category == sqlmodel.TypeCategoryArray && arrays < sqlmodel.MaxTypeDepth {
		arrays++
		element = element.ElementType
	}
	if arrays == 0 || element == nil || element.Category != sqlmodel.TypeCategoryStruct || isNamedTuple(element) || !chvalues.HoldsJSON(element) {
		return 0
	}
	return arrays
}

// documentMapProblems reports Map-of-JSON columns the native driver cannot read safely.
//
// Nested Maps and overridden Map columns are rejected under Q059 because they cannot use
// entry-by-entry reading (clickhouse-go v2.48).
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func documentMapProblems(query *sqlmodel.AnalysedQuery) []emit.Problem {
	problems := make([]emit.Problem, 0, len(query.OutputColumns))
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		var message string
		switch {
		case chvalues.NestsJSONMap(&column.SQLType):
			message = fmt.Sprintf("column %s holds a Map whose values are JSON documents inside an Array, a Map or a Tuple, which "+
				"the native ClickHouse driver panics reading; select the Map as a column of its own, or cast it to a Map of Nullable(JSON)",
				column.Name)
		case column.GoTypeOverride != nil && chvalues.MapsJSON(&column.SQLType):
			message = fmt.Sprintf("column %s is a Map whose values are JSON documents, which the native ClickHouse driver reads only "+
				"entry by entry into the generated map type; remove the column's Go type override", column.Name)
		default:
			continue
		}
		problems = append(problems, emit.Problem{Query: query, Code: diag.CodeEmitterUnsupported, Message: message, Severity: diag.SeverityError})
	}
	return problems
}

// holdsDriverValues reports nested driver values requiring conversion to generated Go
// types.
//
// Unnamed tuple interface elements retain driver values, except JSON documents that need
// conversion to the package's JSON type. A value of unknown type, such as a Dynamic,
// counts wherever it is: the driver scans such a value only into the Go type of the value
// it holds or into a sql.Scanner, never into an interface value.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
// Takes place (valuePlace) which is where the value sits.
// Takes depth (int) which is how deep the type is nested.
//
// Returns bool which is true when the SQL type contains such a value.
func holdsDriverValues(sqlType *sqlmodel.SQLType, place valuePlace, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryNetwork:
		return sqlType.Network == sqlmodel.NetworkKindAddress && place != placeColumn
	case sqlmodel.TypeCategoryInteger:
		return sqlType.Width > bitsInLong && !sqlType.Nullable && place == placeField
	case sqlmodel.TypeCategoryArray:
		return holdsDriverValues(sqlType.ElementType, placeElement, depth+1)
	case sqlmodel.TypeCategoryMap:
		return mapHoldsDriverValues(sqlType, depth)
	case sqlmodel.TypeCategoryStruct:
		return tupleHoldsDriverValues(sqlType, depth) || place != placeField && !isNamedTuple(sqlType) && chvalues.HoldsJSON(sqlType)
	case sqlmodel.TypeCategoryJSON:
		return place != placeColumn
	case sqlmodel.TypeCategoryUnknown:
		return true
	default:
		return false
	}
}

// mapHoldsDriverValues reports whether a Map's keys or values hold a value the native
// driver hands in a Go type of its own, or its values hold a named tuple, which it reads
// only as a map[string]any.
//
// Takes sqlType (*sqlmodel.SQLType) which is the Map's type.
// Takes depth (int) which is how deep the type is nested.
//
// Returns bool which is true when the Map holds such a value.
func mapHoldsDriverValues(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType.ElementType != nil && holdsNamedTuple(sqlType.ElementType, depth+1) {
		return true
	}
	return holdsDriverValues(sqlType.KeyType, placeElement, depth+1) || holdsDriverValues(sqlType.ElementType, placeElement, depth+1)
}

// tupleHoldsDriverValues reports whether a named tuple's elements hold a value the native
// driver hands in a Go type of its own. The elements of an unnamed tuple are read into
// interface values, so nothing inside one counts.
//
// Takes sqlType (*sqlmodel.SQLType) which is the tuple's type.
// Takes depth (int) which is how deep the type is nested.
//
// Returns bool which is true when the tuple holds such a value.
func tupleHoldsDriverValues(sqlType *sqlmodel.SQLType, depth int) bool {
	if !isNamedTuple(sqlType) {
		return false
	}
	for index := range sqlType.StructFields {
		if holdsDriverValues(&sqlType.StructFields[index].SQLType, placeField, depth+1) {
			return true
		}
	}
	return false
}

// holdsNamedTuple reports whether a type is a named tuple, or holds one as an element of
// an Array or a value of a Map.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
// Takes depth (int) which is how deep the type is nested.
//
// Returns bool which is true when the SQL type contains a named tuple.
func holdsNamedTuple(sqlType *sqlmodel.SQLType, depth int) bool {
	if depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryStruct:
		return isNamedTuple(sqlType)
	case sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryMap:
		return sqlType.ElementType != nil && holdsNamedTuple(sqlType.ElementType, depth+1)
	default:
		return false
	}
}

// isNamedTuple reports whether a type is a tuple whose elements all have names, which the
// native driver reads as a map keyed by element name and the generated code as a struct.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for a named tuple.
func isNamedTuple(sqlType *sqlmodel.SQLType) bool {
	if sqlType.Category != sqlmodel.TypeCategoryStruct || len(sqlType.StructFields) == 0 {
		return false
	}
	for index := range sqlType.StructFields {
		if sqlType.StructFields[index].Positional || sqlType.StructFields[index].Name == "" {
			return false
		}
	}
	return true
}
