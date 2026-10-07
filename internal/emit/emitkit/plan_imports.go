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
	"maps"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// goTypePointer starts the name of a pointer Go type.
	goTypePointer = "*"

	// goTypeSlice starts the name of a slice Go type.
	goTypeSlice = "[]"
)

// goTypeUse records the Go types a package's generated code may use: the import paths
// they come from, and whether any is the inline JSON type.
type goTypeUse struct {
	// packages holds the import path of every Go type used.
	packages map[string]struct{}

	// inlineJSON is true when a Go type used is the inline JSON type.
	inlineJSON bool
}

// addModels records the Go type of every column a model holds.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables, or nil.
// Takes resolver (sqlmodel.GoTypeResolver) which maps SQL types, or nil.
func (u *goTypeUse) addModels(catalogue *sqlmodel.Catalogue, resolver sqlmodel.GoTypeResolver) {
	for _, table := range catalogueTables(catalogue) {
		for index := range table.Columns {
			column := &table.Columns[index]
			if column.Hidden {
				continue
			}
			nullable := column.Nullable
			if column.NullableOverride != nil {
				nullable = *column.NullableOverride
			}
			u.resolve(resolver, column.ValueType(), nullable, column.GoTypeOverride)
		}
	}
}

// addQueries records the Go type of every parameter, except the pagination bounds, which
// bind as int, and of every output column of the queries.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes resolver (sqlmodel.GoTypeResolver) which maps SQL types, or nil.
func (u *goTypeUse) addQueries(queries []*sqlmodel.AnalysedQuery, resolver sqlmodel.GoTypeResolver) {
	for _, query := range queries {
		for index := range query.Parameters {
			parameter := &query.Parameters[index]
			if !parameter.IsPaginationBound() {
				u.resolve(resolver, parameter.SQLType, parameter.Nullable, parameter.GoTypeOverride)
			}
		}
		for index := range query.OutputColumns {
			column := &query.OutputColumns[index]
			u.resolve(resolver, column.SQLType, column.Nullable, column.GoTypeOverride)
		}
	}
}

// resolve records the Go type a value maps to: its override when it has one, and
// otherwise the type the mapping gives it.
//
// Takes resolver (sqlmodel.GoTypeResolver) which maps SQL types, or nil.
// Takes sqlType (sqlmodel.SQLType) which is the value's SQL type.
// Takes nullable (bool) which selects the nullable Go type.
// Takes override (*sqlmodel.GoType) which replaces the mapping, or nil.
func (u *goTypeUse) resolve(resolver sqlmodel.GoTypeResolver, sqlType sqlmodel.SQLType, nullable bool, override *sqlmodel.GoType) {
	switch {
	case override != nil:
		u.add(*override)
	case resolver != nil:
		u.add(resolver.Resolve(sqlType, nullable))
	}
}

// add records a Go type and the types it is built from.
//
// Takes goType (sqlmodel.GoType) which is the Go type.
func (u *goTypeUse) add(goType sqlmodel.GoType) {
	if goType.Package != "" {
		u.packages[goType.Package] = struct{}{}
	} else if baseGoTypeName(goType.Name) == emit.InlineJSONTypeName && goType.Composite == nil {
		u.inlineJSON = true
	}
	if goType.Composite == nil {
		return
	}
	for _, part := range []*sqlmodel.GoType{goType.Composite.Key, goType.Composite.Value} {
		if part != nil {
			u.add(*part)
		}
	}
	for index := range goType.Composite.Fields {
		u.add(goType.Composite.Fields[index].Type)
	}
	for _, argument := range goType.Composite.Arguments {
		u.add(argument)
	}
}

// usedGoTypes resolves the Go type of every model column, query parameter and output
// column of a request, and the JSON scan adapter, to find the packages they come from.
//
// Takes request (*emit.Request) which carries the catalogue, queries, type mapping and
// style.
//
// Returns goTypeUse which records the Go types found.
func usedGoTypes(request *emit.Request) goTypeUse {
	use := goTypeUse{packages: make(map[string]struct{}), inlineJSON: false}
	use.add(request.Style.JSONScanInto)
	use.addModels(request.Catalogue, request.Types)
	use.addQueries(request.Queries, request.Types)
	return use
}

// baseGoTypeName strips the pointer and slice markers from a Go type's name.
//
// Takes name (string) which is the Go type's name.
//
// Returns string which is the name of the type it is built on.
func baseGoTypeName(name string) string {
	for {
		switch {
		case strings.HasPrefix(name, goTypePointer):
			name = name[len(goTypePointer):]
		case strings.HasPrefix(name, goTypeSlice):
			name = name[len(goTypeSlice):]
		default:
			return name
		}
	}
}

// planReservedImports chooses the qualifier of every import path the emitter reserves, in
// the emitter's order, claimed in the package scope: of two paths implying the same
// package name, the first keeps it.
//
// Takes reserved ([]string) which are the emitter's import paths in order.
// Takes names (map[string]string) which maps import paths to the names their packages
// declare, or is nil.
// Takes scope (*nameScope) which is the package scope the qualifiers are claimed in.
//
// Returns map[string]string which maps each import path to its qualifier.
func planReservedImports(reserved []string, names map[string]string, scope *nameScope) map[string]string {
	qualifiers := make(map[string]string, len(reserved))
	for _, importPath := range reserved {
		if _, planned := qualifiers[importPath]; !planned {
			qualifiers[importPath] = scope.claim(importQualifier(importPath, names))
		}
	}
	return qualifiers
}

// importQualifier returns the qualifier an import path's package is referred to by,
// before any number keeps it clear of another: the name the style gives the package, or
// the name its path implies.
//
// Takes importPath (string) which is the import path.
// Takes names (map[string]string) which maps import paths to the names their packages
// declare, as emit.Style.PackageNames holds them, or is nil.
//
// Returns string which is the qualifier.
func importQualifier(importPath string, names map[string]string) string {
	if name, named := names[importPath]; named && name != "" {
		return name
	}
	return goast.ImportQualifier(importPath)
}

// planUsedImports adds the qualifier of every import path the mapped Go types come from
// and the emitter does not reserve, by path, claimed in the package scope clear of every
// name a method body declares, since a body that declared one would hide the package.
//
// Takes qualifiers (map[string]string) which maps the paths planned so far to their
// qualifiers; it is updated.
// Takes used (map[string]struct{}) which holds the import paths of the Go types used.
// Takes names (map[string]string) which maps import paths to the names their packages
// declare, or is nil.
// Takes scope (*nameScope) which is the package scope the qualifiers are claimed in.
// Takes body (*nameScope) which holds the names a method body declares.
func planUsedImports(qualifiers map[string]string, used map[string]struct{}, names map[string]string, scope, body *nameScope) {
	for _, importPath := range slices.Sorted(maps.Keys(used)) {
		if _, planned := qualifiers[importPath]; !planned {
			qualifiers[importPath] = claimClearOf(scope, body, importQualifier(importPath, names))
		}
	}
}

// claimClearOf claims a name in a scope that another scope does not hold either: the name
// itself, or the first of name2, name3 and so on that both leave free.
//
// Takes scope (*nameScope) which is the scope the name is claimed in.
// Takes other (*nameScope) which holds names to avoid as well.
// Takes name (string) which is the name wanted.
//
// Returns string which is the name claimed.
func claimClearOf(scope, other *nameScope, name string) string {
	candidate := name
	for suffix := 2; scope.has(candidate) || other.has(candidate); suffix++ {
		candidate = name + strconv.Itoa(suffix)
	}
	scope.take(candidate)
	return candidate
}
