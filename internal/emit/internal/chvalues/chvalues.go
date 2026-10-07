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
	"embed"
	"strings"

	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// File is the generated file holding the value helper.
	File = "clickhouse_values.go"

	// sourceName is the name of the helper's source file.
	sourceName = "source/clickhouse_values.go"

	// AppendingFile is the generated file holding the conversion of the values a native
	// batch appends, which reads its shapes with the value helper's parser, so a package
	// holding it also holds File.
	AppendingFile = "clickhouse_appending.go"

	// appendingSourceName is the name of the conversion's source file.
	appendingSourceName = "source/clickhouse_appending.go"

	// BatchParameters bounds the named values one multi-row batch statement binds.
	//
	// The server's cost for a statement grows faster than the number of its query parameters
	// (a thousand take tens of milliseconds, sixty-five thousand take minutes), so a batch
	// splits into statements of this many values rather than of the bind-variable ceiling.
	BatchParameters = 1024
)

var (
	// Helpers lists the helper-prefix suffixes of the identifiers the value helper's file
	// declares, so an emitter can reserve them.
	Helpers = []string{
		"ClickHouseComposite", "ClickHouseEpoch", "ClickHouseFollow", "ClickHouseHasText",
		"ClickHouseIsNumber", "ClickHouseIsNumberText", "ClickHouseMaxDepth", "ClickHouseMethods",
		"ClickHouseMoment", "ClickHouseNanosecondDigits", "ClickHouseDigitGroup", "ClickHouseReadElement",
		"ClickHouseNumber", "ClickHouseParseShape", "ClickHouseQuotedEscaper", "ClickHouseReadElements",
		"ClickHouseReadName", "ClickHouseReadShape", "ClickHouseRefusalCleaner", "ClickHouseResolve",
		"ClickHouseScalar", "ClickHouseShape", "ClickHouseText", "ClickHouseTextEscaper", "ClickHouseTime",
		"ClickHouseTupleElements", "ClickHouseUnsigned", "ClickHouseValue", "ClickHouseWhole",
		"ClickHouseAddress", "ClickHouseShapes", "ClickHouseShapedValue",
		"ClickHouseAppendElement", "ClickHouseAppendNumber", "ClickHouseAppendComposite",
		"ClickHouseAppendArray", "ClickHouseAppendMap", "ClickHouseAppendTuple",
	}

	// EncodedHelpers lists the helper-prefix suffixes of the identifiers the value helper's
	// file declares besides Helpers when a JSON parameter is bound as the document its value
	// encodes to.
	EncodedHelpers = []string{"ClickHouseEncoded", "ClickHouseEncodedDocument"}

	// NullableHelpers lists the helper-prefix suffixes of the identifiers the value helper's
	// file declares besides Helpers when a runtime builder binds a nullable parameter held
	// through a pointer.
	NullableHelpers = []string{suffixNullable}

	// DurationHelpers lists the helper-prefix suffixes of the identifiers the value helper's
	// file declares besides Helpers when a parameter holds a time of day, which is bound
	// from a time.Duration.
	DurationHelpers = []string{"ClickHouseDuration"}

	// Sentinels lists the suffixes of the unexported sentinel errors the value helper's file
	// declares.
	Sentinels = []string{"ClickHouseShape", "ClickHouseValue"}

	// AppendingHelpers lists the helper-prefix suffixes of the identifiers the conversion's
	// file declares, so the native emitter can reserve them.
	AppendingHelpers = []string{
		suffixAppendRow, "AppendNativeShapedRow", "AppendNativeValue", "AppendNativeHolds", "AppendNativeChecked", "AppendNativeDriverMap",
		"AppendNative", "AppendNativeAddressKind", "AppendNativeAddress", "AppendNativeType", "AppendNativeTarget",
		"AppendNativeTuple", "AppendNativePointer", "AppendNativeList", "AppendNativeMap", "AppendNativeFields",
		"AppendNativeItem", "AppendNativeEntry", "AppendNativeFieldName", "AppendNativeUnknown", "AppendNativeBasic",
		"AppendNativeConverted", "AppendNativeKindType", "AppendNativeConvertedAt", "AppendNativeFieldsChange",
		"AppendNativeContainer",
	}

	// AppendingSentinels lists the suffixes of the unexported sentinel errors the
	// conversion's file declares.
	AppendingSentinels = []string{"ClickHouseAppend"}
)

// source holds the sources of the value helper, the conversion and the reading of JSON
// documents.
//
//go:embed source/clickhouse_values.go source/clickhouse_appending.go
//go:embed source/clickhouse_documents.go
var source embed.FS

// preparedSources caches immutable parsed helper templates for this emitter.
var preparedSources = helpers.NewSources(source, "source")

// ValueParts says which optional helpers clickhouse_values.go holds.
type ValueParts struct {
	// Shapes holds descriptors collected while emitting the generated package's queries.
	Shapes *Shapes

	// Documents includes sglClickHouseEncoded, which binds a JSON parameter as the document
	// its value encodes to.
	Documents bool

	// Nullables includes sglClickHouseNullable, which hands a runtime builder's nullable
	// parameter held through a pointer to the driver.
	Nullables bool

	// Durations includes sglClickHouseDuration, which writes a time.Duration bound to a Time
	// or Time64 parameter as the text ClickHouse parses for it.
	Durations bool
}

// Values renders clickhouse_values.go with the helper that formats named parameter values
// as ClickHouse text. The selected parts add helpers for encoded JSON documents, nullable
// pointers in runtime builders and time of day values.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
// Takes parts (ValueParts) which says which optional helpers the file holds.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be read or rendered.
func Values(vocabulary helpers.Vocabulary, packageName string, parts ValueParts) (sqlmodel.GeneratedFile, error) {
	shapes, err := parts.Shapes.code(vocabulary)
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, err
	}
	return renderData(File, sourceName, vocabulary, &helpers.Data{
		Flags:   map[string]bool{"json": parts.Documents, "positional": parts.Nullables, "durations": parts.Durations},
		Values:  map[string]any{"writesDurations": parts.Durations, "shapes": shapes},
		Renames: nil,
		Package: packageName,
	})
}

// Appending renders clickhouse_appending.go: sglAppendNativeRow, which checks every IP
// address the values of a row a native batch appends hold against its type and converts a
// value holding a Map into the driver's own Go types, before appending the row.
//
// It reads its shapes with the value helper's parser, so the package also needs File.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be read or rendered.
func Appending(vocabulary helpers.Vocabulary, packageName string) (sqlmodel.GeneratedFile, error) {
	return render(AppendingFile, appendingSourceName, vocabulary, packageName)
}

// render renders one of the embedded sources into a generated file.
//
// Takes name (string) which is the generated file's name.
// Takes sourcePath (string) which is the source's path among the embedded sources.
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be read or rendered.
func render(name, sourcePath string, vocabulary helpers.Vocabulary, packageName string) (sqlmodel.GeneratedFile, error) {
	return renderData(name, sourcePath, vocabulary, &helpers.Data{Flags: nil, Values: nil, Renames: nil, Package: packageName})
}

// renderData renders one of the embedded sources into a generated file with the flags and
// values of its directives.
//
// Takes name (string) which is the generated file's name.
// Takes sourcePath (string) which is the source's path among the embedded sources.
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes data (*helpers.Data) which holds the flags, values and package name.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be read or rendered.
func renderData(name, sourcePath string, vocabulary helpers.Vocabulary, data *helpers.Data) (sqlmodel.GeneratedFile, error) {
	return preparedSources.File(name, strings.TrimPrefix(sourcePath, "source/"), vocabulary, data)
}
