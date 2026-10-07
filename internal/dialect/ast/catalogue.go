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

package ast

import "seagull.sh/seagull/internal/sqlmodel"

// FunctionCatalogue holds a dialect's built-in functions, which the analyser consults
// beside the functions the catalogue records.
type FunctionCatalogue struct {
	// Functions maps function names to their overloaded signatures.
	Functions map[string][]*sqlmodel.Function
}

// TypeCatalogue holds a dialect's built-in type definitions.
type TypeCatalogue struct {
	// Types maps normalised type names to their SQLType definitions.
	Types map[string]sqlmodel.SQLType
}

// FunctionResolution is the result of a dialect's own function resolution, returned by
// dialect.FunctionResolverPort.ResolveFunctionCall. It lets a dialect type
// context-dependent or polymorphic functions that the standard overload resolution cannot
// match.
type FunctionResolution struct {
	// ReturnType is the resolved return type.
	ReturnType sqlmodel.SQLType

	// NullingArgument is the zero-based position of the argument whose NULL makes the result
	// NULL, read only when NullableBehaviour is
	// sqlmodel.FunctionNullableNullWhenArgumentNull.
	NullingArgument int

	// NullableBehaviour describes how the function handles NULL arguments.
	NullableBehaviour sqlmodel.FunctionNullableBehaviour

	// DataAccess describes whether the function may modify data.
	DataAccess sqlmodel.FunctionDataAccess

	// IsAggregate indicates whether the function is an aggregate.
	IsAggregate bool

	// ReturnsSet indicates whether a set of rows is returned.
	ReturnsSet bool

	// ElementsKnown reports whether ReturnType determines nullability for array elements,
	// list elements and map values. When true, generated code follows that nullability;
	// false uses the default pointer representation for elements the engine can store as
	// NULL.
	ElementsKnown bool
}
