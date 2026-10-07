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

package gogen

import (
	"seagull.sh/seagull/internal/sqlmodel"
)

// CompositeArrayParameter is a parameter of a query that is an array, through any number
// of dimensions, of a composite type or of a table's row type, which a driver that has
// not registered the type cannot bind.
type CompositeArrayParameter struct {
	// Name is the parameter's name.
	Name string

	// TypeName is the name of the composite type or the table whose rows the elements are.
	TypeName string
}

// CompositeArrayParameters lists the parameters of a query that are arrays, through any
// number of dimensions, of a composite type or of a table's row type, whose Go type no
// override chose, for an emitter whose driver cannot bind them to refuse.
//
// Takes resolver (sqlmodel.GoTypeResolver) which maps the request's SQL types, and says
// which mappings an override chose.
// Takes catalogue (*sqlmodel.Catalogue) which resolves the elements' type names; nil
// resolves none.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []CompositeArrayParameter which holds the parameters, in order, or nil when
// there are none.
func CompositeArrayParameters(resolver sqlmodel.GoTypeResolver, catalogue *sqlmodel.Catalogue,
	query *sqlmodel.AnalysedQuery) []CompositeArrayParameter {
	var found []CompositeArrayParameter
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		if parameter.SQLType.Category != sqlmodel.TypeCategoryArray ||
			OverrideChosen(resolver, parameter.GoTypeOverride, parameter.SQLType) {
			continue
		}
		if typeName, composite := compositeElement(catalogue, parameter.SQLType); composite {
			found = append(found, CompositeArrayParameter{Name: parameter.Name, TypeName: typeName})
		}
	}
	return found
}

// compositeElement reports whether an array's elements, through any number of dimensions,
// are of a composite type or of the row type of a table the catalogue holds.
//
// Takes catalogue (*sqlmodel.Catalogue) which resolves the elements' type name; nil
// resolves none.
// Takes sqlType (sqlmodel.SQLType) which is the array's type.
//
// Returns string which is the elements' type name.
// Returns bool which is true for an array of a composite type or a row type.
func compositeElement(catalogue *sqlmodel.Catalogue, sqlType sqlmodel.SQLType) (string, bool) {
	element := sqlType
	for depth := 0; depth < sqlmodel.MaxTypeDepth && element.Category == sqlmodel.TypeCategoryArray && element.ElementType != nil; depth++ {
		element = *element.ElementType
	}
	if catalogue != nil {
		element = catalogue.ResolveNamedType(element)
	}
	switch {
	case element.Category == sqlmodel.TypeCategoryComposite:
		return element.EngineName, true
	case element.Category == sqlmodel.TypeCategoryUnknown && element.EngineName != "" && catalogue != nil:
		return element.EngineName, catalogue.LookupTable(element.Schema, element.EngineName) != nil
	default:
		return "", false
	}
}
