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

package dialect

import "seagull.sh/seagull/internal/sqlmodel"

// Dialect is a SQL dialect as the generator and the tools around it hold one: the facts
// generated code reads about it, and how it names its types.
//
// A built-in engine's New and the registry's New make one. The zero Dialect holds no
// dialect: the generator refuses it, its Info is the zero Facts, and every type it names
// is of the unknown category.
//
// Dialect implements TypeNormaliserPort for use with live schema sources.
type Dialect struct {
	// port is the dialect behind the handle, or nil for the zero Dialect.
	port DialectPort
}

var _ TypeNormaliserPort = Dialect{}

// Info returns the facts generated code and tools read about the dialect.
//
// Returns Facts which belong to the caller, or the zero Facts for the zero Dialect.
func (handle Dialect) Info() Facts {
	if handle.port == nil {
		return Facts{}
	}
	info := handle.port.Info()
	return info.Facts()
}

// NormaliseTypeName converts a dialect-specific type name and optional modifiers
// (precision, scale, length) into a structured SQLType, as the dialect reads a column's
// type.
//
// Takes name (string) which is the dialect-native type name, such as varchar, numeric or
// int8.
// Takes modifiers (...int) which are optional type parameters such as precision, scale,
// or length.
//
// Returns sqlmodel.SQLType which is the structured type, or a type of the unknown
// category named name for the zero Dialect.
func (handle Dialect) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	if handle.port == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name)
	}
	return handle.port.NormaliseTypeName(name, modifiers...)
}

// Wrap returns the Dialect that holds a dialect.
//
// Takes port (DialectPort) which is the dialect, or nil for the zero Dialect.
//
// Returns Dialect which holds it.
func Wrap(port DialectPort) Dialect {
	return Dialect{port: port}
}

// Unwrap returns the dialect a Dialect holds.
//
// Takes handle (Dialect) which holds the dialect.
//
// Returns DialectPort which is the dialect, or nil for the zero Dialect.
func Unwrap(handle Dialect) DialectPort {
	return handle.port
}
