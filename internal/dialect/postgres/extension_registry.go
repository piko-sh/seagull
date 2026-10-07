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

package postgres

import (
	"slices"
	"sync"

	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// typeUUID is the SQL type descriptor for the uuid type.
	typeUUID = sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "uuid")

	// typeText is the SQL type descriptor for the text type.
	typeText = sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")

	// typeBytea is the SQL type descriptor for the bytea type.
	typeBytea = sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "bytea")

	// typeFloat8 is the SQL type descriptor for the float8 type.
	typeFloat8 = sqlmodel.NewFloatType("float8", widthDouble)

	// typeJSON is the SQL type descriptor for the json type.
	typeJSON = sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json")

	// typeInteger is the SQL type descriptor for the int4 type.
	typeInteger = sqlmodel.NewIntegerType("int4", widthInteger, false)

	// typeTextList is the SQL type descriptor the hstore and pg_trgm key and trigram
	// functions return, a text-category type spelt text[].
	typeTextList = sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text[]")

	// extensionRegistry holds the function signatures each supported Postgres extension adds
	// when installed, keyed by extension name. It is built on the first lookup.
	extensionRegistry = sync.OnceValue(buildExtensionRegistry)
)

// lookupExtensionFunctions returns the function signatures registered for an extension by
// name.
//
// Takes name (string) which is the extension name.
//
// Returns []*sqlmodel.Function which is the list of function signatures, or nil when no
// extension matches.
func lookupExtensionFunctions(name string) []*sqlmodel.Function {
	return extensionRegistry()[name]
}

// buildExtensionRegistry builds the function signatures of every supported Postgres
// extension.
//
// Returns map[string][]*sqlmodel.Function which maps each extension's name to the
// signatures it adds.
func buildExtensionRegistry() map[string][]*sqlmodel.Function {
	return map[string][]*sqlmodel.Function{
		"pgcrypto": {
			extensionFunction("gen_random_uuid", nil, typeUUID, sqlmodel.FunctionNullableNeverNull),
			extensionFunction("crypt", unnamedArguments(typeText, typeText), typeText, sqlmodel.FunctionNullableReturnsNullOnNull),
			extensionFunction("digest", unnamedArguments(typeText, typeText), typeBytea, sqlmodel.FunctionNullableReturnsNullOnNull),
			extensionFunction("gen_salt", unnamedArguments(typeText), typeText, sqlmodel.FunctionNullableReturnsNullOnNull,
				sqlmodel.WithMinArguments(1)),
			extensionFunction("hmac", unnamedArguments(typeText, typeText, typeText), typeBytea, sqlmodel.FunctionNullableReturnsNullOnNull),
		},
		"uuid-ossp": {
			extensionFunction("uuid_generate_v1", nil, typeUUID, sqlmodel.FunctionNullableNeverNull),
			extensionFunction("uuid_generate_v1mc", nil, typeUUID, sqlmodel.FunctionNullableNeverNull),
			extensionFunction("uuid_generate_v3", unnamedArguments(typeUUID, typeText), typeUUID, sqlmodel.FunctionNullableNeverNull),
			extensionFunction("uuid_generate_v4", nil, typeUUID, sqlmodel.FunctionNullableNeverNull),
			extensionFunction("uuid_generate_v5", unnamedArguments(typeUUID, typeText), typeUUID, sqlmodel.FunctionNullableNeverNull),
			extensionFunction("uuid_nil", nil, typeUUID, sqlmodel.FunctionNullableNeverNull),
		},
		"pg_trgm": {
			extensionFunction("similarity", unnamedArguments(typeText, typeText), typeFloat8, sqlmodel.FunctionNullableReturnsNullOnNull),
			extensionFunction("word_similarity", unnamedArguments(typeText, typeText), typeFloat8, sqlmodel.FunctionNullableReturnsNullOnNull),
			extensionFunction("strict_word_similarity", unnamedArguments(typeText, typeText), typeFloat8,
				sqlmodel.FunctionNullableReturnsNullOnNull),
			extensionFunction("show_trgm", unnamedArguments(typeText), typeTextList, sqlmodel.FunctionNullableReturnsNullOnNull),
		},
		"hstore": {
			extensionFunction("akeys", unnamedArguments(typeText), typeTextList, sqlmodel.FunctionNullableReturnsNullOnNull),
			extensionFunction("avals", unnamedArguments(typeText), typeTextList, sqlmodel.FunctionNullableReturnsNullOnNull),
			extensionFunction("hstore_to_json", unnamedArguments(typeText), typeJSON, sqlmodel.FunctionNullableReturnsNullOnNull),
		},
		"ltree": {
			extensionFunction("nlevel", unnamedArguments(typeText), typeInteger, sqlmodel.FunctionNullableReturnsNullOnNull),
			extensionFunction("lca", unnamedArguments(typeText, typeText), typeText, sqlmodel.FunctionNullableReturnsNullOnNull,
				sqlmodel.WithVariadic(1)),
		},
	}
}

// extensionFunction builds a named extension function signature, marked read-only.
//
// Every registered extension function is a scalar that reads but never modifies table
// data, so a pure SELECT using one (similarity(), gen_random_uuid(), nlevel(), ...)
// classifies as read-only and can be routed to a read replica. Left at DataAccessUnknown,
// the call-graph propagation would treat it as data-modifying.
//
// Takes name (string) which is the function's name.
// Takes arguments ([]sqlmodel.FunctionArgument) which are its arguments, or nil.
// Takes returnType (sqlmodel.SQLType) which is the type it returns.
// Takes nullableBehaviour (sqlmodel.FunctionNullableBehaviour) which says when the result
// is NULL.
// Takes options (...sqlmodel.FunctionOption) which set further attributes.
//
// Returns *sqlmodel.Function which is the configured signature.
func extensionFunction(
	name string,
	arguments []sqlmodel.FunctionArgument,
	returnType sqlmodel.SQLType,
	nullableBehaviour sqlmodel.FunctionNullableBehaviour,
	options ...sqlmodel.FunctionOption,
) *sqlmodel.Function {
	options = append([]sqlmodel.FunctionOption{sqlmodel.WithFunctionName(name)}, options...)
	function := sqlmodel.NewFunction(arguments, returnType, nullableBehaviour, options...)
	function.DataAccess = sqlmodel.DataAccessReadOnly
	return function
}

// unnamedArguments builds required, unnamed arguments of the given types, the shape every
// registered extension function declares.
//
// Takes types (...sqlmodel.SQLType) which are the argument types in order.
//
// Returns []sqlmodel.FunctionArgument which holds one required argument per type.
func unnamedArguments(types ...sqlmodel.SQLType) []sqlmodel.FunctionArgument {
	arguments := make([]sqlmodel.FunctionArgument, len(types))
	for index := range types {
		arguments[index] = sqlmodel.FunctionArgument{
			Type:       types[index],
			Name:       "",
			IsOptional: false,
			NotNull:    false,
		}
	}
	return arguments
}

// copyExtensionFunctions copies registered extension signatures, so the copies share
// nothing with the registry. A registered signature's types are flat values holding no
// nested type, list or pointer, so copying each signature and its argument list copies
// every part of it.
//
// Takes registered ([]*sqlmodel.Function) which are the registered signatures.
//
// Returns []*sqlmodel.Function which holds a copy of each, or nil when registered is nil.
func copyExtensionFunctions(registered []*sqlmodel.Function) []*sqlmodel.Function {
	if registered == nil {
		return nil
	}
	functions := make([]*sqlmodel.Function, len(registered))
	for index, function := range registered {
		copied := *function
		copied.Arguments = slices.Clone(function.Arguments)
		functions[index] = &copied
	}
	return functions
}
