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

package mariadb

import (
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// decodeOracleMinimumArguments is the fewest arguments DECODE_ORACLE takes: the value,
	// one search and its result.
	decodeOracleMinimumArguments = 3

	// percentileArguments is the number of arguments a percentile function takes: the
	// fraction and the value its WITHIN GROUP clause sorts.
	percentileArguments = 2
)

var (
	// mariaDBTypes holds MariaDB's additional UUID, address and vector column types.
	//
	// INET4 and INET6 are text; VECTOR values are packed binary data.
	mariaDBTypes = map[string]sqlmodel.SQLType{
		"uuid":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "uuid"),
		"inet4":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "inet4"),
		"inet6":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "inet6"),
		"vector": sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "vector"),
	}

	// mySQLOnlyFunctions names the MySQL built-in functions MariaDB 11 rejects as unknown,
	// those it takes fewer arguments of, which registerNarrowerFunctions registers again
	// with MariaDB's arguments, and the address tests MariaDB types differently, which
	// registerAddressTests registers again.
	mySQLOnlyFunctions = []string{
		"any_value", "grouping", "regexp_like", "uuid_to_bin", "bin_to_uuid", "is_uuid",
		"json_storage_size", "json_schema_validation_report",
		"regexp_instr", "regexp_substr", "regexp_replace", "lag", "lead",
		"is_ipv4", "is_ipv6", "is_ipv4_mapped", "is_ipv4_compat",
	}
)

// New creates a MariaDB dialect using the MySQL parser.
//
// It applies MariaDB's types, functions and syntax rules. Caller options are applied
// afterwards, allowing further configuration and overrides of MariaDB's defaults.
//
// Takes options (...mysql.Option) which further configure the dialect after the MariaDB
// defaults.
//
// Returns *mysql.Dialect which is the MariaDB dialect.
func New(options ...mysql.Option) *mysql.Dialect {
	return mysql.New(append([]mysql.Option{
		mysql.WithName("mariadb"),
		mysql.WithExtraTypes(mariaDBTypes),
		mysql.WithReturning(ast.DataChangeInsert | ast.DataChangeDelete),
		mysql.WithJSONArrows(false),
		mysql.WithInsertRowAlias(false),
		mysql.WithJSONValueReturning(false),
		mysql.WithUngroupedColumns(dialect.UngroupedColumnsNullable),
		mysql.WithSequences(true),
		mysql.WithOffsetFetch(true),
		mysql.WithCreateOrReplace(true),
		mysql.WithoutBuiltinFunctions(mySQLOnlyFunctions...),
		mysql.WithExtraFunctions(registerMariaDBFunctions),
		mysql.WithFunctionCallHook(shiftParameterIntoString),
		mysql.WithFunctionCallHook(typeDiscretePercentile),
	}, options...)...)
}

// shiftParameterIntoString resolves MariaDB date arithmetic over parameters as text. The
// shifted value is the one MySQL's own typing of the call follows, as
// mysql.ShiftedDateArgument reports it.
//
// Invalid date values may produce NULL. Other calls fall through to MySQL's rules.
//
// Takes name (string) which is the lower-cased function name.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns *ast.FunctionResolution which types the call, or nil to decline.
func shiftParameterIntoString(name string, argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	shifted, shifts := mysql.ShiftedDateArgument(name)
	if !shifts || len(argumentTypes) <= shifted || argumentTypes[shifted].Category != sqlmodel.TypeCategoryUnknown {
		return nil
	}
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// typeDiscretePercentile types PERCENTILE_DISC as the value its WITHIN GROUP clause
// sorts, which the parser passes as its second argument: it returns one of the values, or
// NULL over a window of NULLs. Other calls fall through.
//
// Takes name (string) which is the lower-cased function name.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns *ast.FunctionResolution which types the call, or nil to decline.
func typeDiscretePercentile(name string, argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if name != "percentile_disc" || len(argumentTypes) != percentileArguments {
		return nil
	}
	return &ast.FunctionResolution{
		ReturnType:        argumentTypes[1],
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// registerMariaDBFunctions registers MariaDB-specific built-in functions onto the shared
// MySQL function catalogue.
//
// INET6_ATON / INET6_NTOA are standard MySQL functions and live in the base catalogue, so
// only genuinely MariaDB-exclusive functions are registered here: SYS_GUID, the JSON
// functions MySQL lacks, the Oracle-compatible functions MariaDB accepts in its default
// SQL mode, such as NVL, and its other own functions. JSON_QUERY is NULL when the path is
// missing or selects a scalar, so it may be NULL even over a non-null document.
//
// Takes builder (*mysql.FunctionCatalogueBuilder) which receives the extra function
// signatures.
func registerMariaDBFunctions(builder *mysql.FunctionCatalogueBuilder) {
	builder.NeverNull("sys_guid", nil, builder.Varchar())
	document := mysql.Arg{Name: "json", Type: builder.JSON()}
	path := mysql.Arg{Name: "path", Type: builder.Text()}
	builder.CalledOnNull("json_query", builder.Args(document, path), builder.JSON())
	builder.NullOnNull("json_exists", builder.Args(document, path), builder.Boolean())
	builder.NullOnNull("json_equals", builder.Args(document, mysql.Arg{Name: "other", Type: builder.JSON()}), builder.Boolean())
	for _, name := range []string{"json_detailed", "json_loose", "json_compact", "json_normalize"} {
		builder.NullOnNull(name, builder.Args(document), builder.JSON())
	}
	registerMariaDBJSONFunctions(builder, document, path)
	registerOracleCompatibleFunctions(builder)
	registerMariaDBStringFunctions(builder)
	registerSequenceFunctions(builder)
	registerNarrowerFunctions(builder)
	registerAddressTests(builder)
}

// registerSequenceFunctions registers MariaDB's sequence functions, whose first argument
// names the sequence, which the parser reads as text, as it reads NEXT VALUE FOR and
// PREVIOUS VALUE FOR as NEXTVAL and LASTVAL.
//
// NEXTVAL advances the sequence, so it changes data, and never gives NULL: an exhausted
// sequence fails the statement. LASTVAL is NULL until the session reads the sequence.
// SETVAL changes data, and is NULL when it does not move the sequence, as for a value
// below its next one; its optional arguments say whether the value counts as read and in
// which round of a cycling sequence it falls.
//
// Takes builder (*mysql.FunctionCatalogueBuilder) which receives the signatures.
func registerSequenceFunctions(builder *mysql.FunctionCatalogueBuilder) {
	sequence := mysql.Arg{Name: "sequence", Type: builder.Text()}
	value := mysql.Arg{Name: "value", Type: builder.Bigint()}
	builder.ModifiesData("nextval", builder.Args(sequence), builder.Bigint())
	builder.CalledOnNull("lastval", builder.Args(sequence), builder.Bigint())
	setval := builder.ModifiesData("setval", builder.Args(sequence, value, mysql.Arg{Name: "is_used", Type: builder.Any()},
		mysql.Arg{Name: "round", Type: builder.Bigint()}), builder.Bigint())
	setval.NullableBehaviour = sqlmodel.FunctionNullableCalledOnNull
	setval.MinArguments = 2
}

// registerAddressTests adds MariaDB's IP-address predicate signatures.
//
// Their results are integers and remain non-null even for NULL arguments.
//
// Takes builder (*mysql.FunctionCatalogueBuilder) which receives the signatures.
func registerAddressTests(builder *mysql.FunctionCatalogueBuilder) {
	text := builder.Args(mysql.Arg{Name: "expression", Type: builder.Text()})
	binary := builder.Args(mysql.Arg{Name: "expression", Type: builder.Varbinary()})
	builder.NeverNull("is_ipv4", text, builder.Integer())
	builder.NeverNull("is_ipv6", text, builder.Integer())
	builder.NeverNull("is_ipv4_mapped", binary, builder.Integer())
	builder.NeverNull("is_ipv4_compat", binary, builder.Integer())
}

// registerNarrowerFunctions adds MariaDB signatures with fewer arguments than MySQL
// accepts.
//
// It also records the nullability of regular-expression and window-function results.
//
// Takes builder (*mysql.FunctionCatalogueBuilder) which receives the signatures.
func registerNarrowerFunctions(builder *mysql.FunctionCatalogueBuilder) {
	subject := mysql.Arg{Name: "expression", Type: builder.Text()}
	pattern := mysql.Arg{Name: "pattern", Type: builder.Text()}
	builder.NullOnNull("regexp_instr", builder.Args(subject, pattern), builder.Integer())
	builder.CalledOnNull("regexp_substr", builder.Args(subject, pattern), builder.Text())
	builder.NullOnNull("regexp_replace", builder.Args(subject, pattern, mysql.Arg{Name: "replacement", Type: builder.Text()}), builder.Text())
	value := mysql.Arg{Name: "expression", Type: builder.Any()}
	offset := mysql.Arg{Name: "offset", Type: builder.Integer()}
	for _, name := range []string{"lag", "lead"} {
		shifted := builder.CalledOnNull(name, builder.Args(value, offset), builder.Any())
		shifted.MinArguments = 1
	}
}

// registerMariaDBJSONFunctions registers the JSON functions MariaDB 11 adds. Each is NULL
// for a document of the wrong shape, such as JSON_OBJECT_TO_ARRAY of an array, so each
// may be NULL over non-null arguments.
//
// Takes builder (*mysql.FunctionCatalogueBuilder) which receives the signatures.
// Takes document (mysql.Arg) which is the JSON document argument.
// Takes path (mysql.Arg) which is the JSON path argument.
func registerMariaDBJSONFunctions(builder *mysql.FunctionCatalogueBuilder, document mysql.Arg, path mysql.Arg) {
	other := mysql.Arg{Name: "other", Type: builder.JSON()}
	builder.CalledOnNull("json_array_intersect", builder.Args(document, other), builder.JSON())
	builder.CalledOnNull("json_object_filter_keys", builder.Args(document, other), builder.JSON())
	builder.CalledOnNull("json_object_to_array", builder.Args(document), builder.JSON())
	builder.CalledOnNull("json_key_value", builder.Args(document, path), builder.JSON())
}

// registerOracleCompatibleFunctions registers the Oracle-compatible functions MariaDB
// accepts in its default SQL mode.
//
// NVL is IFNULL, NULL only when both arguments are; NVL2 and DECODE_ORACLE return one of
// their arguments, which the dialect's function resolver types. The _ORACLE variants of
// the string functions treat an empty string as NULL, so each may be NULL over non-null
// arguments.
//
// Takes builder (*mysql.FunctionCatalogueBuilder) which receives the signatures.
func registerOracleCompatibleFunctions(builder *mysql.FunctionCatalogueBuilder) {
	value := mysql.Arg{Name: "value", Type: builder.Any()}
	other := mysql.Arg{Name: "other", Type: builder.Any()}
	fallback := mysql.Arg{Name: "fallback", Type: builder.Any()}
	text := mysql.Arg{Name: "string", Type: builder.Text()}
	length := mysql.Arg{Name: "length", Type: builder.Integer()}
	builder.CalledOnNull("nvl", builder.Args(value, other), builder.Any())
	builder.CalledOnNull("nvl2", builder.Args(value, other, fallback), builder.Any())
	builder.Variadic("decode_oracle", builder.Args(value, other, fallback), decodeOracleMinimumArguments, builder.Any())
	builder.Variadic("concat_operator_oracle", builder.Args(value), 1, builder.Text())
	for _, name := range []string{"ltrim_oracle", "rtrim_oracle", "trim_oracle"} {
		builder.CalledOnNull(name, builder.Args(text), builder.Text())
	}
	for _, name := range []string{"lpad_oracle", "rpad_oracle"} {
		builder.CalledOnNull(name, builder.Args(text, length), builder.Text())
		builder.CalledOnNull(name, builder.Args(text, length, mysql.Arg{Name: "pad", Type: builder.Text()}), builder.Text())
	}
	builder.CalledOnNull("replace_oracle", builder.Args(text, mysql.Arg{Name: "from_string", Type: builder.Text()},
		mysql.Arg{Name: "to_string", Type: builder.Text()}), builder.Text())
	position := mysql.Arg{Name: "position", Type: builder.Integer()}
	builder.CalledOnNull("substr_oracle", builder.Args(text, position), builder.Text())
	builder.CalledOnNull("substr_oracle", builder.Args(text, position, length), builder.Text())
	builder.NullOnNull("add_months", builder.Args(mysql.Arg{Name: "date", Type: builder.Datetime()},
		mysql.Arg{Name: "months", Type: builder.Integer()}), builder.Datetime())
	builder.NullOnNull("to_char", builder.Args(value), builder.Text())
	builder.NullOnNull("to_char", builder.Args(value, mysql.Arg{Name: "format", Type: builder.Text()}), builder.Text())
	builder.NullOnNull("lengthb", builder.Args(text), builder.Integer())
	builder.NullOnNull("chr", builder.Args(mysql.Arg{Name: "code", Type: builder.Integer()}), builder.Text())
}

// registerMariaDBStringFunctions registers the other functions MariaDB 11 has and MySQL
// lacks: identifiers, checksums, formatting, key derivation and vectors, and the MEDIAN,
// PERCENTILE_CONT and PERCENTILE_DISC window functions, which are NULL over a window of
// NULLs; the percentiles take the value their WITHIN GROUP clause sorts as their second
// argument.
//
// Takes builder (*mysql.FunctionCatalogueBuilder) which receives the signatures.
func registerMariaDBStringFunctions(builder *mysql.FunctionCatalogueBuilder) {
	text := mysql.Arg{Name: "string", Type: builder.Text()}
	unsigned := builder.Type("int unsigned")
	builder.NeverNull("uuid_v4", nil, builder.Text())
	builder.NeverNull("uuid_v7", nil, builder.Text())
	builder.NullOnNull("crc32", builder.Args(mysql.Arg{Name: "seed", Type: builder.Bigint()}, text), unsigned)
	builder.NullOnNull("crc32c", builder.Args(text), unsigned)
	builder.NullOnNull("crc32c", builder.Args(mysql.Arg{Name: "seed", Type: builder.Bigint()}, text), unsigned)
	builder.NullOnNull("natural_sort_key", builder.Args(text), builder.Text())
	builder.Variadic("sformat", builder.Args(mysql.Arg{Name: "format", Type: builder.Text()}, mysql.Arg{Name: "value", Type: builder.Any()}),
		1, builder.Text())
	derivation := builder.NullOnNull("kdf", builder.Args(mysql.Arg{Name: "key", Type: builder.Text()},
		mysql.Arg{Name: "salt", Type: builder.Text()}, mysql.Arg{Name: "info", Type: builder.Text()},
		mysql.Arg{Name: "kdf_name", Type: builder.Text()}, mysql.Arg{Name: "width", Type: builder.Integer()}), builder.Varbinary())
	derivation.MinArguments = 2
	vector := mysql.Arg{Name: "vector", Type: builder.Varbinary()}
	other := mysql.Arg{Name: "other", Type: builder.Varbinary()}
	builder.NullOnNull("vec_fromtext", builder.Args(text), builder.Varbinary())
	builder.NullOnNull("vec_totext", builder.Args(vector), builder.Text())
	builder.NullOnNull("vec_distance_euclidean", builder.Args(vector, other), builder.Double())
	builder.NullOnNull("vec_distance_cosine", builder.Args(vector, other), builder.Double())
	builder.CalledOnNull("median", builder.Args(mysql.Arg{Name: "value", Type: builder.Any()}), builder.Double())
	fraction := mysql.Arg{Name: "fraction", Type: builder.Double()}
	sorted := mysql.Arg{Name: "value", Type: builder.Any()}
	builder.CalledOnNull("percentile_cont", builder.Args(fraction, sorted), builder.Double())
	builder.CalledOnNull("percentile_disc", builder.Args(fraction, sorted), builder.Any())
}
