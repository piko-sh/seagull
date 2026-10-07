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

package databasesql

import (
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// driverPgx is the module of the pgx driver, which database/sql reaches through its
	// stdlib package.
	driverPgx = "github.com/jackc/pgx/v5"

	// driverMySQL is the module of the go-sql-driver MySQL driver.
	driverMySQL = "github.com/go-sql-driver/mysql"

	// driverDuckDB is the module of the DuckDB driver.
	driverDuckDB = "github.com/duckdb/duckdb-go/v2"

	// driverClickHouse is the module of the clickhouse-go driver, whose database/sql bridge
	// binds named placeholders from clickhouse.Named values.
	driverClickHouse = "github.com/ClickHouse/clickhouse-go/v2"

	// driverModernc is the module of the pure Go SQLite driver.
	driverModernc = "modernc.org/sqlite"

	// driverMattn is the module of the cgo SQLite driver.
	driverMattn = "github.com/mattn/go-sqlite3"

	// importShopspringDecimal is the decimal package clickhouse-go/v2 scans Decimal columns
	// into. It is written into generated code only.
	importShopspringDecimal = "github.com/shopspring/decimal"

	// importMathBig is the standard library package for integers wider than 64 bits.
	importMathBig = "math/big"

	// importTime is the standard library package whose Duration type clickhouse-go reads a
	// Time or Time64 value into.
	importTime = "time"

	// importNet is the standard library package whose IP type clickhouse-go's database/sql
	// bridge returns for an IP address.
	importNet = "net"
)

// driverProfile records how one driver behind database/sql binds and scans values: the
// facts the generated code must follow that the dialect does not decide.
type driverProfile struct {
	// buildsElement reports whether the driver builds a SQL value nested in a LIST, ARRAY,
	// MAP or STRUCT parameter from its Go value. Nil accepts all elements; lists containing
	// unsupported elements use inference, while maps or structs containing them are refused.
	buildsElement func(element *sqlmodel.SQLType) bool

	// module is the driver's module path, empty for the general profile.
	module string

	// types holds the Go types the driver scans and binds for the structural types whose
	// built-in defaults it cannot round-trip; a zero slot keeps the default.
	types sqlmodel.TypeSlots

	// readsAsJSON lists the type categories whose values the driver cannot scan into the Go
	// slice, map or struct the mapping chooses, which the generated code wraps in the
	// dialect's to-JSON function and decodes.
	readsAsJSON []sqlmodel.SQLTypeCategory

	// readsConverted lists SQL type categories that require conversion from driver-native
	// values to generated Go types.
	readsConverted []sqlmodel.SQLTypeCategory

	// maxBatchParameters bounds the arguments one multi-row batch statement binds below the
	// dialect's limit, or is zero to use the dialect's limit alone.
	maxBatchParameters int

	// enums says where the driver, through database/sql, handles the enum types a package
	// declares only as Go's own strings.
	enums gogen.EnumStrings

	// bindsNamedValues reports that the driver binds a dialect's named placeholders from
	// clickhouse.Named values formatted as the server parses them, and binds a batch by
	// naming each row's placeholders apart.
	bindsNamedValues bool

	// preparesOnlyInserts reports that the driver prepares only INSERT statements
	// (clickhouse-go answers PrepareContext on anything else with "invalid INSERT query"),
	// so the PreparedDBTX, which prepares every static query when it starts, is not
	// generated.
	preparesOnlyInserts bool

	// reportsRowsAffected reports that the driver's sql.Result counts the rows a statement
	// changed; clickhouse-go reports none, so execrows and execresult are refused.
	reportsRowsAffected bool

	// bindsJSONThroughValuer enables wrappers that bind JSON values through driver.Valuer.
	bindsJSONThroughValuer bool

	// bindsCompositesAsDriverValues enables conversion to driver-specific MAP, STRUCT and
	// LIST forms.
	bindsCompositesAsDriverValues bool

	// bindsNilSlicesAsEmpty reports that the driver binds a nil Go slice as an empty list
	// rather than NULL, so a nullable list parameter goes through the driver-composite
	// conversion, which binds a nil slice as NULL as every other driver does.
	bindsNilSlicesAsEmpty bool

	// bindsDefinedTypesAsIs enables conversion of defined basic Go types to the predeclared
	// types the driver accepts.
	bindsDefinedTypesAsIs bool

	// bindsBytesAsBinary enables text wrappers for overridden text parameters holding bytes.
	bindsBytesAsBinary bool

	// bindsDecomposedDecimals reports that the driver binds pointers with a decimal
	// Decompose method through driver.Valuer. Otherwise these parameters need a wrapper to
	// avoid rejection or incorrect NULL values.
	bindsDecomposedDecimals bool

	// readsDecimalsAsText reports that the driver returns an exact decimal in a type of its
	// own the decimal slot cannot scan, so the generated code casts it to the dialect's text
	// type and scans the text.
	readsDecimalsAsText bool

	// readsIntervalsAsText reports that the driver returns an interval in a type of its own
	// the interval slot cannot scan, so the generated code casts it to the dialect's text
	// type and scans the text.
	readsIntervalsAsText bool

	// readsJSONAsText reports that the driver decodes a JSON value into Go's own values, a
	// JSON string into an unquoted Go string and a number into a float64, so the generated
	// code casts a JSON column to the dialect's text type and scans the document's own text.
	readsJSONAsText bool

	// readsTupleElementsAsInterfaces marks drivers that lose unnamed tuples nested within
	// other tuples. Map values read through concrete Go types are exempt.
	readsTupleElementsAsInterfaces bool

	// readsDocuments enables conversion of nested driver-specific JSON documents.
	readsDocuments bool

	// panicsOnDocumentMaps reports that the driver panics returning a Map whose values are
	// JSON documents, at any depth of a column, so such a column is refused.
	panicsOnDocumentMaps bool

	// readsNestedUnknownsInItsOwnTypes reports that the driver returns nested unknown values
	// in its own slices or maps. Such columns need a converting adapter because database/sql
	// cannot store those containers into the generated Go types.
	readsNestedUnknownsInItsOwnTypes bool

	// refusesCompositeArrays reports that the driver cannot bind an array of a composite
	// type or of a table's row type, which database/sql passes it as a slice of Go values,
	// so such a parameter is refused.
	refusesCompositeArrays bool

	// refusesNullElements reports that the driver cannot bind a NULL element of a list
	// parameter or a NULL value of a map parameter, so a parameter's elements are plain
	// values, and a parameter declaring them nullable is refused.
	refusesNullElements bool
}

var (
	// duckDBUnbuiltElements lists the categories of the elements the DuckDB driver cannot
	// build inside a LIST, ARRAY, MAP or STRUCT parameter from the Go values the generated
	// code binds.
	duckDBUnbuiltElements = []sqlmodel.SQLTypeCategory{
		sqlmodel.TypeCategoryEnum, sqlmodel.TypeCategoryDecimal, sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryUUID,
	}

	// documentHolders lists the type categories a driver that reads documents as its own
	// returns a JSON document inside of, decoded.
	documentHolders = []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryMap, sqlmodel.TypeCategoryStruct}

	// clickHouseEnums says where clickhouse-go's database/sql bridge handles the enum types
	// a package declares only as Go's own strings: database/sql stores the string it returns
	// for an enum value into a named string type, but the slices and maps it returns only
	// into a slice or map of exactly their type.
	clickHouseEnums = gogen.EnumStrings{Scan: gogen.EnumSlice | gogen.EnumMap | gogen.EnumNested | gogen.EnumNestedInMap, Bind: 0}

	// driverProfiles holds what the emitter knows of each driver behind database/sql, keyed
	// by the driver's module path. A driver missing from it gets the general profile, which
	// keeps the built-in behaviour.
	driverProfiles = map[string]driverProfile{
		driverPgx:        pgxProfile(),
		driverMySQL:      mysqlProfile(),
		driverDuckDB:     duckDBProfile(),
		driverClickHouse: clickHouseProfile(),
		driverModernc:    sqliteProfile(),
		driverMattn:      sqliteProfile(),
	}

	// generalProfile is the profile of a driver the emitter knows nothing of: the built-in
	// type defaults, and arrays read as JSON to avoid depending on a driver's array
	// representation.
	generalProfile = driverProfile{
		module:                           "",
		types:                            sqlmodel.TypeSlots{},
		bindsNamedValues:                 false,
		preparesOnlyInserts:              false,
		reportsRowsAffected:              true,
		maxBatchParameters:               0,
		bindsJSONThroughValuer:           false,
		bindsCompositesAsDriverValues:    false,
		bindsNilSlicesAsEmpty:            false,
		bindsDefinedTypesAsIs:            false,
		bindsBytesAsBinary:               false,
		bindsDecomposedDecimals:          false,
		readsAsJSON:                      []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryArray},
		readsConverted:                   nil,
		readsDecimalsAsText:              false,
		readsIntervalsAsText:             false,
		readsJSONAsText:                  false,
		readsTupleElementsAsInterfaces:   false,
		readsDocuments:                   false,
		panicsOnDocumentMaps:             false,
		readsNestedUnknownsInItsOwnTypes: false,
		refusesCompositeArrays:           false,
		refusesNullElements:              false,
		buildsElement:                    nil,
		enums:                            gogen.EnumStrings{},
	}

	// textSlot is the slot of a value the driver returns as text: string, or a pointer to it
	// when nullable.
	textSlot = sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: "", Name: "string"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: "", Name: "*string"},
	}

	// timeSlot is the slot of a value the driver returns as a time.Time: time.Time, or a
	// pointer to it when nullable.
	timeSlot = sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: "time", Name: "Time"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: "time", Name: "*Time"},
	}

	// bigIntegerSlot is the slot of an integer wider than 64 bits that the driver returns as
	// a *big.Int, nullable or not.
	bigIntegerSlot = sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: importMathBig, Name: "*Int"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: importMathBig, Name: "*Int"},
	}
)

// reading returns how the generated code reads a column of a SQL type on the driver.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns gogen.ColumnReading which says how the column is read.
func (profile *driverProfile) reading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	switch {
	case slices.Contains(profile.readsConverted, sqlType.Category):
		return gogen.ColumnReadConverted
	case profile.readsDocuments && slices.Contains(documentHolders, sqlType.Category) && chvalues.HoldsJSON(&sqlType):
		return gogen.ColumnReadConverted
	case profile.readsNestedUnknownsInItsOwnTypes && holdsNestedUnknown(&sqlType, 0):
		return gogen.ColumnReadConverted
	case slices.Contains(profile.readsAsJSON, sqlType.Category):
		return gogen.ColumnReadAsJSON
	case profile.readsAsText(sqlType):
		return gogen.ColumnReadAsText
	default:
		return gogen.ColumnReadAsIs
	}
}

// readsAsText reports whether the driver returns a value of a SQL type in a type of its
// own that the generated code reads as text instead: a decimal, an interval or a JSON
// document, as the profile says.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns bool which is true when the column is cast to text.
func (profile *driverProfile) readsAsText(sqlType sqlmodel.SQLType) bool {
	switch {
	case sqlType.Category == sqlmodel.TypeCategoryDecimal:
		return profile.readsDecimalsAsText
	case sqlType.Category == sqlmodel.TypeCategoryJSON:
		return profile.readsJSONAsText
	case sqlType.Temporal == sqlmodel.TemporalKindInterval:
		return profile.readsIntervalsAsText
	default:
		return false
	}
}

// holdsNestedUnknown reports whether a type is an array or a map holding, as an element
// or a value at any depth, a value of unknown type; the elements of a tuple, which a
// driver reads into interface values, do not count.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
// Takes depth (int) which bounds the walk.
//
// Returns bool which is true when the array or map holds such a value.
func holdsNestedUnknown(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryMap:
		element := sqlType.ElementType
		return element != nil && (element.Category == sqlmodel.TypeCategoryUnknown || holdsNestedUnknown(element, depth+1))
	default:
		return false
	}
}

// profileFor returns the profile of the driver the emitter generates for: the named
// driver when one was given, and otherwise the first of the dialect's drivers the emitter
// has a profile for. A dialect listing none of them gets the general profile.
//
// Takes drivers ([]string) which lists the modules of the dialect's drivers, preferred
// first.
// Takes driver (string) which is the module of the driver to use, or empty.
//
// Returns driverProfile which is the driver's profile.
func profileFor(drivers []string, driver string) driverProfile {
	if driver != "" {
		if profile, found := driverProfiles[driver]; found {
			profile.module = driver
			return profile
		}
		return generalProfile
	}
	for _, candidate := range drivers {
		if profile, found := driverProfiles[candidate]; found {
			profile.module = candidate
			return profile
		}
	}
	return generalProfile
}

// pgxProfile describes pgx's behaviour through database/sql.
//
// It reads time-of-day and interval values as text. Enum slices bind as ordinary strings;
// unsupported enum nesting is rejected before generation. It binds a decimal held by a
// pointer with a Decompose method, which database/sql hands it as it is, through the
// value's driver.Valuer.
//
// Returns driverProfile which is the profile.
func pgxProfile() driverProfile {
	profile := generalProfile
	profile.types.TimeOfDay = textSlot
	profile.enums = gogen.EnumStrings{Scan: 0, Bind: gogen.EnumSlice | gogen.EnumNested}
	profile.refusesCompositeArrays = true
	profile.bindsDecomposedDecimals = true
	return profile
}

// mysqlProfile returns the go-sql-driver MySQL profile, which reads TIME as text because
// it may exceed a day or be negative. DATETIME, DATE and TIMESTAMP require parseTime=true
// in the DSN to scan into time.Time.
//
// Returns driverProfile which is the profile.
func mysqlProfile() driverProfile {
	profile := generalProfile
	profile.types.TimeOfDay = textSlot
	return profile
}

// duckDBProfile returns the profile of the DuckDB driver.
//
// It returns an integer wider than 64 bits as a *big.Int, and a time of day, zone-aware
// or not, as a time.Time, so both read into one. It returns a list as a []any, a struct
// as a map keyed by field name and a map as its own duckdb.OrderedMap, each holding its
// own Go values, which database/sql cannot store into a typed slice, map or struct, so
// those are read through the driver-composite scan adapter, which converts them (the
// dialect refuses a column holding a map keyed by a list, a struct, a map or a union,
// which the driver cannot read at all). It returns a decimal and an interval as its own
// Decimal and Interval types, which the decimal and interval slots cannot scan, and a
// JSON value decoded into Go's own values, a JSON string as an unquoted Go string, so
// those are read as text. It binds a JSON value only as text or bytes and binds a named
// slice type without consulting its driver.Valuer, so JSON parameters go through the
// driver-value wrapper, and a list of them through the driver-composite conversion, since
// the driver panics on a list element of a type it does not know. It binds a MAP
// parameter only from its own duckdb.Map or duckdb.OrderedMap and a STRUCT only from a
// map keyed by field name, so map, struct and list-of-struct parameters are converted
// into those forms; and it binds a nil slice as an empty list, so a nullable list
// parameter is converted too, binding nil as NULL. It binds a value of a defined type as
// it is, refusing a defined []byte type such as json.RawMessage and panicking on a slice
// of a defined string type, and binds a []byte as a BLOB, which a VARCHAR will not
// compare with, so a parameter whose Go type an override chose is converted too. It
// panics on a NULL element of a list parameter, or a NULL value of a map parameter, and
// on a pointer to one, so parameters bind plain elements and values. It builds a LIST,
// ARRAY, MAP or STRUCT parameter from the type the statement gives it, which fails with
// "unsupported data type" on an element of an enum, a DECIMAL, a BLOB, a UUID or an
// INTERVAL (see duckDBBuildsElement), but binds a list a driver.Valuer returns by the
// types of its Go elements, which DuckDB casts to the parameter's type, so such a list
// goes through the list-inference wrapper; it infers no map or struct, so a MAP or STRUCT
// parameter holding such an element is refused.
//
// Returns driverProfile which is the profile.
func duckDBProfile() driverProfile {
	profile := generalProfile
	profile.types.BigInt = bigIntegerSlot
	profile.types.TimeOfDay = timeSlot
	profile.readsAsJSON = nil
	profile.readsConverted = []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryMap, sqlmodel.TypeCategoryStruct}
	profile.readsDecimalsAsText = true
	profile.readsIntervalsAsText = true
	profile.readsJSONAsText = true
	profile.bindsJSONThroughValuer = true
	profile.bindsCompositesAsDriverValues = true
	profile.bindsNilSlicesAsEmpty = true
	profile.bindsDefinedTypesAsIs = true
	profile.bindsBytesAsBinary = true
	profile.refusesNullElements = true
	profile.buildsElement = duckDBBuildsElement
	return profile
}

// duckDBBuildsElement reports whether DuckDB builds a nested parameter element from its
// generated Go value. It refuses enums, DECIMAL, BLOB, UUID and INTERVAL values that the
// driver cannot convert inside LIST, ARRAY, MAP or STRUCT parameters.
//
// Takes element (*sqlmodel.SQLType) which is the element's type.
//
// Returns bool which is true when the driver builds the element.
func duckDBBuildsElement(element *sqlmodel.SQLType) bool {
	return !slices.Contains(duckDBUnbuiltElements, element.Category) && element.Temporal != sqlmodel.TemporalKindInterval
}

// sqliteProfile returns the SQLite drivers' profile, including text binding for
// byte-backed overrides.
//
// Returns driverProfile which is the profile.
func sqliteProfile() driverProfile {
	profile := generalProfile
	profile.bindsBytesAsBinary = true
	return profile
}

// clickHouseProfile returns the profile of clickhouse-go's database/sql bridge.
//
// It binds named placeholders from clickhouse.Named values, prepares only INSERT
// statements, reports no count of the rows a statement changed and scans arrays itself.
// It returns an enum array as a []string, an array of nullable enums as a []*string and a
// map of enum values as a map of strings, which database/sql stores only into a slice or
// map of exactly that type, so the generated code reads them through Go's own strings,
// and refuses an enum held deeper. A batch statement binds at most
// chvalues.BatchParameters values. It scans a decimal into a shopspring decimal and binds
// one through its fmt.Stringer form, returns an integer wider than 64 bits as a *big.Int,
// a named tuple as map[string]any and an IP address as a net.IP, which database/sql
// cannot store into a Go struct or a netip.Addr, and a nullable one as a *net.IP, which
// it stores NULL into, where a net.IP takes no NULL and a slice or map of net.IP no
// *net.IP. It returns a Time or Time64 value as a time.Duration. It reads every element
// of a tuple into an interface value, where an inner tuple whose elements have no names
// loses them, so a column holding one is refused.
//
// Returns driverProfile which is the profile.
func clickHouseProfile() driverProfile {
	decimal := sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: importShopspringDecimal, Name: "Decimal"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: importShopspringDecimal, Name: "*Decimal"},
	}
	address := sqlmodel.GoType{Composite: nil, Package: importNet, Name: "IP"}
	optionalAddress := sqlmodel.GoType{Composite: nil, Package: importNet, Name: "*IP"}
	fields := sqlmodel.NewGoMap(sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}, sqlmodel.GoType{Composite: nil, Package: "", Name: "any"})
	types := sqlmodel.TypeSlots{}
	types.Decimal = decimal
	types.Money = decimal
	types.BigInt = bigIntegerSlot
	types.IPAddress = sqlmodel.TypeSlot{NotNull: address, Nullable: optionalAddress}
	types.Struct = sqlmodel.TypeSlot{NotNull: fields, Nullable: fields}
	types.TimeOfDay = sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: importTime, Name: "Duration"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: importTime, Name: "*Duration"},
	}
	return driverProfile{
		module:                           "",
		types:                            types,
		bindsNamedValues:                 true,
		preparesOnlyInserts:              true,
		reportsRowsAffected:              false,
		maxBatchParameters:               chvalues.BatchParameters,
		bindsJSONThroughValuer:           false,
		bindsCompositesAsDriverValues:    false,
		bindsNilSlicesAsEmpty:            false,
		bindsDefinedTypesAsIs:            false,
		bindsBytesAsBinary:               false,
		bindsDecomposedDecimals:          false,
		readsAsJSON:                      nil,
		readsConverted:                   nil,
		readsDecimalsAsText:              false,
		readsIntervalsAsText:             false,
		readsJSONAsText:                  false,
		readsTupleElementsAsInterfaces:   true,
		readsDocuments:                   true,
		panicsOnDocumentMaps:             true,
		readsNestedUnknownsInItsOwnTypes: true,
		refusesCompositeArrays:           false,
		refusesNullElements:              false,
		buildsElement:                    nil,
		enums:                            clickHouseEnums,
	}
}

// bindsNamedValues reports whether the generated code binds each parameter as a named
// value: the dialect executes named placeholders and the driver binds them from named
// values.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes placeholders (dialect.Placeholders) which describes the dialect's placeholders.
//
// Returns bool which is true when parameters bind by name.
func bindsNamedValues(profile *driverProfile, placeholders dialect.Placeholders) bool {
	return profile.bindsNamedValues && placeholders.BindsByName()
}

// maxParameters returns the most parameters one statement of the dialect binds, or the
// conservative default when the dialect sets no limit.
//
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns int which is the limit.
func maxParameters(info *dialect.Facts) int {
	if info.MaxParameters > 0 {
		return info.MaxParameters
	}
	return goast.DefaultMaxBindVariablesFallback
}
