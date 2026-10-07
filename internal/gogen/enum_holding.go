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
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// goString is Go's own string type.
	goString = "string"

	// pointerPrefix starts the name of a pointer type.
	pointerPrefix = "*"

	// slicePrefix starts the name of a slice type.
	slicePrefix = "[]"
)

// EnumHolding is how a Go type holds an enum type the package declares: in which shape,
// whether through pointers, the key type of a map holding it, and the enum type's name.
// The zero EnumHolding holds none.
type EnumHolding struct {
	// Key is the key type of a map whose values are the enum type, for EnumMap, and nil
	// otherwise.
	Key *sqlmodel.GoType

	// Name is the enum type's name, the first one the Go type holds when it holds more.
	Name string

	// Shape is the shape the Go type holds the enum type in, or zero when it holds no enum
	// type the package declares.
	Shape EnumShapes

	// Pointer reports a value held through a pointer, as a nullable one is, or, for
	// EnumSlice, a slice whose elements are pointers, as an array of nullable elements is.
	Pointer bool
}

const (
	// stepPointer is a pointer on the way to an enum value.
	stepPointer enumStep = iota

	// stepSlice is a slice's element on the way to an enum value.
	stepSlice

	// stepMapKey is a map's key on the way to an enum value.
	stepMapKey

	// stepMapValue is a map's value on the way to an enum value.
	stepMapValue

	// stepField is a struct's field on the way to an enum value.
	stepField
)

// Strings returns an expression converting a value held as the holding says to Go's own
// string of the same shape: string(value), (*string)(value) or, for a slice or a map, a
// call of its conversion helper, such as sglConvertStrings[string](value).
//
// Takes value (ast.Expr) which reads the value.
// Takes vocabulary (Vocabulary) which names the conversion helpers.
//
// Returns ast.Expr which is the conversion.
func (h EnumHolding) Strings(value ast.Expr, vocabulary Vocabulary) ast.Expr {
	return h.convert(value, goString, vocabulary)
}

// FromStrings returns an expression converting Go's own string of the holding's shape to
// the enum type: Name(value), (*Name)(value) or, for a slice or a map, a call of its
// conversion helper, such as sglConvertStrings[Name](value).
//
// Takes value (ast.Expr) which reads the strings.
// Takes vocabulary (Vocabulary) which names the conversion helpers.
//
// Returns ast.Expr which is the conversion.
func (h EnumHolding) FromStrings(value ast.Expr, vocabulary Vocabulary) ast.Expr {
	return h.convert(value, h.Name, vocabulary)
}

// StringType returns Go's own string type in the holding's shape: string, *string,
// []string, []*string or a map from the key type to string.
//
// Takes imports (*goast.ImportTracker) which collects the import a map's key type needs.
//
// Returns ast.Expr which is the type.
func (h EnumHolding) StringType(imports *goast.ImportTracker) ast.Expr {
	text := ast.Expr(goastutil.CachedIdent(goString))
	if h.Pointer {
		text = &ast.StarExpr{X: text}
	}
	switch h.Shape {
	case EnumSlice:
		return &ast.ArrayType{Elt: text}
	case EnumMap:
		return &ast.MapType{Key: imports.AddType(*h.Key), Value: text}
	default:
		return text
	}
}

// convert returns an expression converting a value of the holding's shape to the same
// shape of another string type.
//
// Takes value (ast.Expr) which reads the value.
// Takes target (string) which names the string type converted to.
// Takes vocabulary (Vocabulary) which names the conversion helpers.
//
// Returns ast.Expr which is the conversion.
func (h EnumHolding) convert(value ast.Expr, target string, vocabulary Vocabulary) ast.Expr {
	switch {
	case h.Shape == EnumSlice && h.Pointer:
		return h.helperCall(SuffixConvertStringPointers, value, target, vocabulary)
	case h.Shape == EnumSlice:
		return h.helperCall(SuffixConvertStrings, value, target, vocabulary)
	case h.Shape == EnumMap:
		return h.helperCall(SuffixConvertStringMap, value, target, vocabulary)
	case h.Pointer:
		return goastutil.CallExpr(&ast.ParenExpr{X: &ast.StarExpr{X: goastutil.CachedIdent(target)}}, value)
	default:
		return goastutil.CallExpr(goastutil.CachedIdent(target), value)
	}
}

// helperCall returns a call of a conversion helper instantiated with the string type
// converted to, such as sglConvertStrings[string](value).
//
// Takes suffix (string) which is the helper's suffix.
// Takes value (ast.Expr) which reads the value.
// Takes target (string) which names the string type converted to.
// Takes vocabulary (Vocabulary) which names the helper.
//
// Returns ast.Expr which is the call.
func (EnumHolding) helperCall(suffix string, value ast.Expr, target string, vocabulary Vocabulary) ast.Expr {
	helper := &ast.IndexExpr{X: goastutil.CachedIdent(vocabulary.Helper(suffix)), Index: goastutil.CachedIdent(target)}
	return goastutil.CallExpr(helper, value)
}

// enumStep is one step from a Go type to an enum value it holds.
type enumStep uint8

// enumFound is one place a Go type holds an enum type the package declares.
type enumFound struct {
	// name is the enum type's name.
	name string

	// path holds the steps from the Go type to the enum value, outermost first.
	path []enumStep
}

// enumWalk finds the places a Go type holds the enum types a package declares.
type enumWalk struct {
	// resolver maps the fields of a struct type the package declares to their Go types.
	resolver sqlmodel.GoTypeResolver

	// found holds the places found so far.
	found []enumFound
}

// visit records the places a part of a Go type holds an enum type the package declares,
// following the part's SQL type through arrays, maps and structs alongside its Go type. A
// part whose Go type a slot, a mapping or an override chose rather than its structure
// holds none.
//
// Takes sqlType (sqlmodel.SQLType) which is the part's SQL type.
// Takes goType (sqlmodel.GoType) which is the part's Go type.
// Takes path ([]enumStep) which holds the steps to the part.
// Takes depth (int) which is the depth of the part.
func (w *enumWalk) visit(sqlType sqlmodel.SQLType, goType sqlmodel.GoType, path []enumStep, depth int) {
	if depth >= sqlmodel.MaxTypeDepth {
		return
	}
	name, pointer := strings.CutPrefix(goType.Name, pointerPrefix)
	if pointer {
		path = slices.Concat(path, []enumStep{stepPointer})
	}
	goType.Name = name
	switch sqlType.Category {
	case sqlmodel.TypeCategoryEnum:
		if packageType(goType) {
			w.found = append(w.found, enumFound{name: name, path: path})
		}
	case sqlmodel.TypeCategoryArray:
		w.visitArray(sqlType, goType, path, depth)
	case sqlmodel.TypeCategoryMap:
		w.visitMap(sqlType, goType, path, depth)
	case sqlmodel.TypeCategoryStruct:
		w.visitStruct(sqlType, goType, path, depth)
	default:
	}
}

// visitArray records the places an array's elements hold an enum type the package
// declares, when its Go type is a slice of their Go type.
//
// Takes sqlType (sqlmodel.SQLType) which is the array's SQL type.
// Takes goType (sqlmodel.GoType) which is its Go type, without a pointer.
// Takes path ([]enumStep) which holds the steps to the array.
// Takes depth (int) which is the depth of the array.
func (w *enumWalk) visitArray(sqlType sqlmodel.SQLType, goType sqlmodel.GoType, path []enumStep, depth int) {
	element, isSlice := strings.CutPrefix(goType.Name, slicePrefix)
	if sqlType.ElementType == nil || !isSlice {
		return
	}
	goType.Name = element
	w.visit(*sqlType.ElementType, goType, slices.Concat(path, []enumStep{stepSlice}), depth+1)
}

// visitMap records the places a map's keys and values hold an enum type the package
// declares, when its Go type is a map of their Go types.
//
// Takes sqlType (sqlmodel.SQLType) which is the map's SQL type.
// Takes goType (sqlmodel.GoType) which is its Go type, without a pointer.
// Takes path ([]enumStep) which holds the steps to the map.
// Takes depth (int) which is the depth of the map.
func (w *enumWalk) visitMap(sqlType sqlmodel.SQLType, goType sqlmodel.GoType, path []enumStep, depth int) {
	composite := goType.Composite
	if composite == nil || composite.Kind != sqlmodel.GoCompositeMap || goType.Name != "" ||
		composite.Key == nil || composite.Value == nil || sqlType.KeyType == nil || sqlType.ElementType == nil {
		return
	}
	w.visit(*sqlType.KeyType, *composite.Key, slices.Concat(path, []enumStep{stepMapKey}), depth+1)
	w.visit(*sqlType.ElementType, *composite.Value, slices.Concat(path, []enumStep{stepMapValue}), depth+1)
}

// visitStruct records the places a struct's fields hold an enum type the package
// declares, when its Go type is a struct of their Go types or the struct type the package
// declares for it, whose fields map as the resolver maps them.
//
// Takes sqlType (sqlmodel.SQLType) which is the struct's SQL type.
// Takes goType (sqlmodel.GoType) which is its Go type, without a pointer.
// Takes path ([]enumStep) which holds the steps to the struct.
// Takes depth (int) which is the depth of the struct.
func (w *enumWalk) visitStruct(sqlType sqlmodel.SQLType, goType sqlmodel.GoType, path []enumStep, depth int) {
	fieldPath := slices.Concat(path, []enumStep{stepField})
	composite := goType.Composite
	switch {
	case composite != nil && composite.Kind == sqlmodel.GoCompositeStruct && goType.Name == "" &&
		len(composite.Fields) == len(sqlType.StructFields):
		for index := range sqlType.StructFields {
			w.visit(sqlType.StructFields[index].SQLType, composite.Fields[index].Type, fieldPath, depth+1)
		}
	case packageType(goType):
		for index := range sqlType.StructFields {
			field := sqlType.StructFields[index].SQLType
			w.visit(field, w.resolver.Resolve(field, field.Nullable), fieldPath, depth+1)
		}
	default:
	}
}

// holding returns how the Go type holds the enum types found in it.
//
// Takes resolved (sqlmodel.GoType) which is the Go type walked, whose key type a map of
// enum values keeps.
//
// Returns EnumHolding which describes the holding, or the zero EnumHolding when no enum
// type was found.
func (w *enumWalk) holding(resolved sqlmodel.GoType) EnumHolding {
	if len(w.found) == 0 {
		return EnumHolding{}
	}
	name := w.found[0].name
	if len(w.found) == 1 {
		path := w.found[0].path
		switch {
		case len(path) == 0:
			return EnumHolding{Key: nil, Name: name, Shape: EnumValue, Pointer: false}
		case slices.Equal(path, []enumStep{stepPointer}):
			return EnumHolding{Key: nil, Name: name, Shape: EnumValue, Pointer: true}
		case slices.Equal(path, []enumStep{stepSlice}):
			return EnumHolding{Key: nil, Name: name, Shape: EnumSlice, Pointer: false}
		case slices.Equal(path, []enumStep{stepSlice, stepPointer}):
			return EnumHolding{Key: nil, Name: name, Shape: EnumSlice, Pointer: true}
		case slices.Equal(path, []enumStep{stepMapValue}):
			return EnumHolding{Key: resolved.Composite.Key, Name: name, Shape: EnumMap, Pointer: false}
		default:
		}
	}
	for _, found := range w.found {
		if slices.Contains(found.path, stepMapKey) || slices.Contains(found.path, stepMapValue) {
			return EnumHolding{Key: nil, Name: name, Shape: EnumNestedInMap, Pointer: false}
		}
	}
	return EnumHolding{Key: nil, Name: name, Shape: EnumNested, Pointer: false}
}

// HeldEnum describes how a generated Go type contains an enum.
//
// Only enums mapped to types declared by the generated package are considered. The result
// distinguishes scalar values, simple containers and deeper nesting.
//
// Takes sqlType (sqlmodel.SQLType) which is the value's SQL type.
// Takes nullable (bool) which reports a nullable value.
// Takes slice (bool) which reports a slice of such values, as a slice parameter is.
// Takes override (*sqlmodel.GoType) which is the value's Go type override, or nil.
// Takes resolver (sqlmodel.GoTypeResolver) which maps SQL types to Go types.
//
// Returns EnumHolding which says how the value holds the enum type, or the zero
// EnumHolding when it holds none.
func HeldEnum(sqlType sqlmodel.SQLType, nullable, slice bool, override *sqlmodel.GoType, resolver sqlmodel.GoTypeResolver) EnumHolding {
	if override != nil || resolver == nil {
		return EnumHolding{}
	}
	resolved := resolver.Resolve(sqlType, nullable)
	var path []enumStep
	if slice {
		path = []enumStep{stepSlice}
	}
	walk := &enumWalk{resolver: resolver, found: nil}
	walk.visit(sqlType, resolved, path, 0)
	return walk.holding(resolved)
}

// packageType reports whether a Go type, without a pointer, names a type of the generated
// package: a bare identifier that is not one of Go's predeclared types.
//
// Takes goType (sqlmodel.GoType) which is the Go type.
//
// Returns bool which is true for a type the package declares.
func packageType(goType sqlmodel.GoType) bool {
	if goType.Package != "" || goType.Composite != nil || !token.IsIdentifier(goType.Name) {
		return false
	}
	return types.Universe.Lookup(goType.Name) == nil
}
