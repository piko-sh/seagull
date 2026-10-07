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

package emitkit

import (
	"slices"
	"unicode"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
)

const (
	// defaultReceiver is the receiver of every built-in emitter's Queries methods.
	defaultReceiver = "queries"

	// sentinelPrefix starts the name of an unexported generated sentinel error, before the
	// helper prefix in Go casing.
	sentinelPrefix = "err"

	// errorValuePrefix starts the name of an exported generated error value, before the
	// error prefix in Go casing.
	errorValuePrefix = "Err"

	// inlineJSONFileSuffix ends the name of the file declaring the inline JSON type, after
	// the helper prefix.
	inlineJSONFileSuffix = "_json.go"
)

var (
	// defaultLocals lists names reserved for the built-in emitters' generated locals.
	//
	// The naming plan avoids these where generated variables could shadow query declarations
	// or arguments. Reservation tests check the list against generated code.
	defaultLocals = []string{
		"args", "batch", "builder", "chunk", "ctx", "db", "end", "entry", "err", "exists", "filter",
		"groupIndex", "item", "offset", "pagination",
		"query", "raw", "results", "row", "rows", "values", "yield",
	}

	// defaultDeclarations lists the package-level identifiers the built-in emitters declare
	// whatever the style: the querier's, the prepared statements', the sortable direction
	// type's and the query-names lookup's.
	defaultDeclarations = []string{
		"DBTX", "New", "NewWithReplica", "OrderAsc", "OrderDesc", "OrderDirection", "Prepare",
		"PreparedDBTX", "Queries", "dbtxWrapper", "preparedTx", "queryNameMap", "queryNamePrefixNames",
		"queryNamePrefixes",
	}

	// defaultHelperSuffixes lists helper declarations named with the configured prefix.
	//
	// Reservation tests check this list against declarations retained from the helper
	// sources.
	defaultHelperSuffixes = []string{
		"AllowedDirections", "AllowedOperators", "AppendNative", "AppendNativeAddress", "AppendNativeAddressKind",
		"AppendNativeBasic", "AppendNativeChecked", "AppendNativeContainer", "AppendNativeConverted", "AppendNativeConvertedAt", "AppendNativeDriverMap",
		"AppendNativeEntry", "AppendNativeFieldName", "AppendNativeFields", "AppendNativeFieldsChange", "AppendNativeHolds",
		"AppendNativeItem", "AppendNativeKindType", "AppendNativeList", "AppendNativeMap", "AppendNativePointer",
		"AppendNativeRow", "AppendNativeShapedRow", "AppendNativeTarget", "AppendNativeTuple", "AppendNativeType", "AppendNativeUnknown",
		"AppendNativeValue", "At", "BoxSlice", "CastAfter", "CastBefore", "CastMiddle", "CastPatterns",
		"CastTypes",
		"CheckBinds", "CloseStatements",
		"ClickHouseAddress",
		"ClickHouseAppendComposite",
		"ClickHouseAppendArray",
		"ClickHouseAppendMap",
		"ClickHouseAppendTuple",
		"ClickHouseAppendElement",
		"ClickHouseAppendNumber",
		"ClickHouseComposite",
		"ClickHouseDigitGroup", "ClickHouseDocumentDepth", "ClickHouseDocumentText", "ClickHouseDuration", "ClickHouseEncoded",
		"ClickHouseEncodedDocument", "ClickHouseEpoch",
		"ClickHouseFollow", "ClickHouseHasText",
		"ClickHouseIsNumber", "ClickHouseIsNumberText", "ClickHouseLoose", "ClickHouseLooseDocument", "ClickHouseLooseInterface",
		"ClickHouseLooseList", "ClickHouseLooseMap", "ClickHouseLooseType", "ClickHouseMaxDepth", "ClickHouseMayHold",
		"ClickHouseMethods",
		"ClickHouseMoment", "ClickHouseNanosecondDigits", "ClickHouseNullable", "ClickHouseNumber", "ClickHouseParseShape",
		"ClickHouseQuotedEscaper", "ClickHouseReadElement", "ClickHouseReadElements", "ClickHouseReadName",
		"ClickHouseReadShape", "ClickHouseRefusalCleaner", "ClickHouseResolve", "ClickHouseScalar",
		"ClickHouseShape", "ClickHouseShapedValue", "ClickHouseShapes", "ClickHouseText", "ClickHouseTextEscaper", "ClickHouseTime",
		"ClickHouseTupleElements", "ClickHouseUnsigned", "ClickHouseValue", "ClickHouseWhole", "ClickHouseWritesDurations",
		"ColumnExpressionRegex", "ConvertStringMap", "ConvertStringPointers", "ConvertStrings", "DecodeEngineJSON", "DecodeJSONColumn", "Dereference", "DriverBasic",
		"DriverBinary", "DriverComposite", "DriverCompositeMap", "DriverCompositeScanner", "DriverCompositeSlice",
		"DriverCompositeStruct", "DriverConvert", "DriverDecimalText", "DriverDecimalValue", "DriverField", "DriverFieldName", "DriverJSON",
		"DriverInferredList", "DriverIntervalClock", "DriverIntervalPart", "DriverIntervalText", "DriverList",
		"DriverMaxDepth", "DriverNumberType", "DriverReadsDocuments", "DriverReadsOwnTypes", "DriverRefusal", "DriverText",
		"DriverTooDeep", "DriverValue", "Edit", "EmptyExclusion", "EmptyMembership", "EngineJSONElements", "EngineJSONText", "EngineJSONValue", "ExtractColumnRoot",
		"Filter", "FilterCount", "FilterInto", "MapEntries", "OrderedMap", "ReadList", "ReadMap", "ReadScalar", "ScalarHolds", "ScalarKind",
		"Unreadable", "IsMultiOperator", "IsUnaryOperator", "JSONDocument", "JSONElementNulls", "JSONNull", "JSONNulls", "JSONScanner",
		"JSONValueNulls", "LimitAfter",
		"LimitBefore", "List", "ListNumbered", "ListPrefix", "ListSuffix", "ListSuffixed",
		"ListMark", "MaxBindVariables", "MaxColumnExpressionLength", "MicrosecondsPerHour",
		"MicrosecondsPerMinute", "MicrosecondsPerSecond", "Minus", "MonthsPerYear", "MultiValueLen", "NewOrder",
		"NewPredicate", "NormaliseDirection", "OffsetAfter", "OffsetBefore", "Order", "OverrideJSONScanner", "OverrideScanner",
		"Pagination", "PaginationCount", "PaginationInto", "Predicate", "ReadClickHouseDocuments", "ReadClickHouseLooseText", "ReadClickHouseText",
		"ReadDriverDecimal", "ReadDriverLeaf", "ReadDriverNull",
		"ReadDriverShape", "ReadDriverStruct", "ReadDriverValue", "ReadOverride",
		"ReadOverrideJSON", "ReadOverrideText", "ReflectSlice", "ResolveColumn", "ScanDriverComposite", "ScanInto",
		"ScanOverride", "ScanOverrideJSON", "SimpleColumn", "Splice", "SplitCast", "UnboundedLimit", "UnmarshalsJSON", "UnmarshalsText",
		"WhereClose", "WhereOpen", "WrapError", "WrapResult", "WriteBindPlaceholder",
	}

	// defaultSentinelSuffixes lists the unexported sentinel errors the built-in emitters
	// name err and the helper prefix in Go casing, so sgl gives errSglClickHouseValue.
	defaultSentinelSuffixes = []string{"ClickHouseAppend", "ClickHouseShape", "ClickHouseValue"}

	// defaultErrorValueSuffixes lists the exported error values the built-in emitters name
	// Err and the error prefix in Go casing, so sgl gives ErrSglUnknownColumn.
	defaultErrorValueSuffixes = []string{"TooManyBindVariables", "UnknownColumn", "UnknownDirection", "UnknownOperator"}

	// defaultMethods lists the methods and fields the built-in emitters declare on Queries.
	defaultMethods = []string{"RunInTx", "WithTx", "db", "reader", "wrapDBTX", "wrapper", "writer"}

	// defaultFiles lists the files the built-in emitters write besides the query files and
	// the inline JSON file, whose name starts with the helper prefix.
	defaultFiles = []string{
		"bind_limits.go", "clickhouse_appending.go", "clickhouse_composites.go", "clickhouse_documents.go", "clickhouse_values.go",
		"convert_strings.go", "driver_composites.go", "error_wrapping.go",
		"driver_values.go", "models.go", "prepared.go", "querier.go", interfaceFile, "runtime_helpers.go",
		"slice_helpers.go", "sortable.go", "value_reading.go",
	}
)

// importList holds import paths in the order their qualifiers are claimed.
type importList []string

// AddImport appends import paths to the list.
//
// Takes paths (...string) which are the import paths, in order.
func (list *importList) AddImport(paths ...string) {
	*list = append(*list, paths...)
}

// DefaultReservation returns the reservation of an emitter that does not implement
// emit.ReserverPort: every name any built-in emitter's generated code uses where a
// planned name could meet it, so a plan made with it is safe for any of them.
//
// It reserves more than one emitter needs, so an emitter that knows its own names should
// implement emit.ReserverPort.
//
// Takes style (*emit.Style) which holds the helper and error prefixes the helper names
// start with. DefaultReservation does not change it.
//
// Returns emit.Reservation which holds the names.
func DefaultReservation(_ *dialect.Facts, style *emit.Style) emit.Reservation {
	declarations := slices.Clone(defaultDeclarations)
	for _, suffix := range defaultHelperSuffixes {
		declarations = append(declarations, style.HelperPrefix+suffix)
	}
	for _, suffix := range defaultSentinelSuffixes {
		declarations = append(declarations, sentinelName(style.HelperPrefix, suffix))
	}
	for _, suffix := range defaultErrorValueSuffixes {
		declarations = append(declarations, errorValuePrefix+goast.SnakeToPascalCase(style.ErrorPrefix)+suffix)
	}
	return emit.Reservation{
		Receiver:     defaultReceiver,
		Locals:       slices.Clone(defaultLocals),
		Imports:      defaultImports(style),
		Declarations: declarations,
		Methods:      slices.Clone(defaultMethods),
		Files:        append(slices.Clone(defaultFiles), style.HelperPrefix+inlineJSONFileSuffix),
	}
}

// ReservationFor returns the names an emitter reserves: those its emit.ReserverPort
// gives, or DefaultReservation's when it does not implement one.
//
// Takes emitter (emit.EmitterPort) which is the emitter.
// Takes info (*dialect.Facts) which describes the dialect. ReservationFor does not change
// it.
// Takes style (*emit.Style) which holds the style of the generated code. ReservationFor
// does not change it.
//
// Returns emit.Reservation which holds the names.
func ReservationFor(emitter emit.EmitterPort, info *dialect.Facts, style *emit.Style) emit.Reservation {
	if reserver, ok := emitter.(emit.ReserverPort); ok {
		return reserver.Reserve(info, style)
	}
	return DefaultReservation(info, style)
}

// defaultImports returns the import paths the built-in emitters' generated files use, the
// standard library first.
//
// Of the two driver packages, database/sql/driver comes first and keeps the qualifier
// driver. A style writing current idioms adds iter, whose Seq2 a stream method returns.
//
// Takes style (*emit.Style) which says whether the code is written in current idioms.
//
// Returns []string which holds the import paths in order.
func defaultImports(style *emit.Style) []string {
	var imports importList
	imports.AddImport("cmp", "context", "database/sql", "database/sql/driver", "encoding/json", "errors", "fmt",
		"math/big", "net", "net/netip", "reflect", "regexp", "slices", "strconv", "strings", "sync", "time")
	imports.AddImport("github.com/ClickHouse/clickhouse-go/v2", "github.com/ClickHouse/clickhouse-go/v2/lib/chcol",
		"github.com/ClickHouse/clickhouse-go/v2/lib/driver",
		"github.com/cockroachdb/apd/v3", "github.com/duckdb/duckdb-go/v2", "github.com/go-sql-driver/mysql",
		"github.com/google/uuid", "github.com/jackc/pgx/v5", "github.com/jackc/pgx/v5/pgconn", "github.com/lib/pq",
		"github.com/shopspring/decimal")
	if style.ModernIdioms {
		imports.AddImport("iter")
	}
	return imports
}

// sentinelName returns the name of an unexported generated sentinel error: err, the
// helper prefix with its first letter upper-cased, and the suffix.
//
// Takes helperPrefix (string) which is the style's helper prefix.
// Takes suffix (string) which ends the name.
//
// Returns string which is the sentinel's name.
func sentinelName(helperPrefix, suffix string) string {
	first, width := utf8.DecodeRuneInString(helperPrefix)
	if width == 0 {
		return sentinelPrefix + suffix
	}
	return sentinelPrefix + string(unicode.ToUpper(first)) + helperPrefix[width:] + suffix
}
