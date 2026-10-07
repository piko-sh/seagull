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

package thirdparty

import (
	"fmt"
	"go/format"
	"go/token"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"seagull.sh/seagull/sdk/emit"
)

const (
	// unnamedPackage is the qualifier of an import whose path implies no usable name.
	unnamedPackage = "pkg"

	// firstNumber is the number the second package wanting a qualifier takes after it.
	firstNumber = 2
)

// source collects one generated Go file: its declarations as text, and the imports they
// use.
type source struct {
	// planned maps each import path the naming plan holds to its qualifier.
	planned map[string]string

	// used maps each import path the declarations refer to to its qualifier.
	used map[string]string

	// body holds the declarations.
	body strings.Builder
}

// newSource returns an empty file of a package planned with a naming plan.
//
// Takes naming (*emit.Naming) which is the package's naming plan, whose Imports gives
// each package its qualifier. newSource does not change it.
//
// Returns *source which is the file.
func newSource(naming *emit.Naming) *source {
	return &source{planned: naming.Imports, used: make(map[string]string), body: strings.Builder{}}
}

// printf writes formatted text to the file's declarations.
//
// Takes layout (string) which is the format.
// Takes arguments (...any) which are the values it formats.
func (file *source) printf(layout string, arguments ...any) {
	_, _ = fmt.Fprintf(&file.body, layout, arguments...)
}

// write writes text to the file's declarations as it is.
//
// Takes texts (...string) which are written in order.
func (file *source) write(texts ...string) {
	for _, text := range texts {
		file.body.WriteString(text)
	}
}

// typeOf writes a Go type, importing the packages it names: the pointer and slice
// prefixes of its name, then its map, struct or generic instance, its qualified name, or
// its name as it is.
//
// Takes goType (emit.GoType) which is the type.
//
// Returns string which is the type's Go source.
func (file *source) typeOf(goType emit.GoType) string {
	name := goType.Name
	for strings.HasPrefix(name, pointerPrefix) || strings.HasPrefix(name, slicePrefix) {
		name = strings.TrimPrefix(strings.TrimPrefix(name, pointerPrefix), slicePrefix)
	}
	modifiers := goType.Name[:len(goType.Name)-len(name)]
	switch {
	case goType.Composite != nil:
		return modifiers + file.compositeOf(goType)
	case goType.Package == "":
		return goType.Name
	default:
		return modifiers + file.qualifier(goType.Package) + "." + name
	}
}

// compositeOf writes the map, generic instance or struct a composite Go type describes.
//
// Takes goType (emit.GoType) which is the type; its Composite is set.
//
// Returns string which is the type's Go source, without its name's prefixes.
func (file *source) compositeOf(goType emit.GoType) string {
	composite := goType.Composite
	switch {
	case composite.Key != nil && composite.Value != nil:
		return "map[" + file.typeOf(*composite.Key) + "]" + file.typeOf(*composite.Value)
	case composite.Value != nil:
		arguments := make([]string, 0, len(composite.Arguments))
		for _, argument := range composite.Arguments {
			arguments = append(arguments, file.typeOf(argument))
		}
		return file.typeOf(*composite.Value) + "[" + strings.Join(arguments, argumentSeparator) + "]"
	}
	var builder strings.Builder
	builder.WriteString("struct {")
	for index, field := range composite.Fields {
		if index > 0 {
			builder.WriteString(";")
		}
		builder.WriteString(" " + field.Name + " " + file.typeOf(field.Type))
		if field.Tag != "" {
			builder.WriteString(" `" + field.Tag + "`")
		}
	}
	builder.WriteString(" }")
	return builder.String()
}

// qualified writes a name an imported package declares, importing the package.
//
// Takes importPath (string) which is the package's import path.
// Takes name (string) which is the name the package declares.
//
// Returns string which is the qualified name, such as sql.ErrNoRows.
func (file *source) qualified(importPath, name string) string {
	return file.qualifier(importPath) + "." + name
}

// qualifier imports a package and returns the qualifier the file refers to it by: the one
// the naming plan gives it, or, for a package the plan does not hold, the name its path
// implies, numbered past every qualifier the plan holds and the file uses.
//
// Takes importPath (string) which is the package's import path.
//
// Returns string which is the qualifier, such as sql.
func (file *source) qualifier(importPath string) string {
	if qualifier, imported := file.used[importPath]; imported {
		return qualifier
	}
	qualifier, planned := file.planned[importPath]
	if !planned {
		qualifier = file.freeQualifier(importPath)
	}
	file.used[importPath] = qualifier
	return qualifier
}

// freeQualifier chooses the qualifier of a package the naming plan does not hold.
//
// Takes importPath (string) which is the package's import path.
//
// Returns string which is the qualifier, taken by no other package of the plan or file.
func (file *source) freeQualifier(importPath string) string {
	name := impliedName(importPath)
	if !token.IsIdentifier(name) {
		name = unnamedPackage
	}
	taken := slices.Concat(slices.Collect(maps.Values(file.planned)), slices.Collect(maps.Values(file.used)))
	candidate := name
	for number := firstNumber; slices.Contains(taken, candidate); number++ {
		candidate = name + strconv.Itoa(number)
	}
	return candidate
}

// declaration writes the file's import declaration: every package it refers to, sorted by
// path, each with an alias where its qualifier is not the name its path implies.
//
// Returns string which is the declaration, or empty when the file imports nothing.
func (file *source) declaration() string {
	if len(file.used) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("import (\n")
	for _, importPath := range slices.Sorted(maps.Keys(file.used)) {
		builder.WriteString("\t")
		if qualifier := file.used[importPath]; qualifier != impliedName(importPath) {
			builder.WriteString(qualifier + " ")
		}
		builder.WriteString(strconv.Quote(importPath) + "\n")
	}
	builder.WriteString(")\n")
	return builder.String()
}

// generated returns the file: the style's header, the package clause, the imports and the
// declarations, formatted as gofmt would.
//
// Takes name (string) which is the file's name.
// Takes request (*emit.Request) which holds the style and the package name.
//
// Returns emit.GeneratedFile which is the file.
// Returns error when the source does not format.
func (file *source) generated(name string, request *emit.Request) (emit.GeneratedFile, error) {
	text := request.Style.Header + "\n\npackage " + request.Package + "\n\n" + file.declaration() + "\n" + file.body.String()
	formatted, err := format.Source([]byte(text))
	if err != nil {
		return emit.GeneratedFile{}, fmt.Errorf("formatting %s: %w", name, err)
	}
	return emit.GeneratedFile{Name: name, Content: formatted}, nil
}

// impliedName returns the package name an import path implies, as goimports assumes it:
// the last element, or the one before a version suffix such as v2, without a go- prefix
// and cut at the first character an identifier cannot hold.
//
// Takes importPath (string) which is the import path.
//
// Returns string which is the name, or empty when the path implies none.
func impliedName(importPath string) string {
	name := path.Base(importPath)
	if version, found := strings.CutPrefix(name, "v"); found && version != "" && strings.Trim(version, "0123456789") == "" {
		if parent := path.Base(path.Dir(importPath)); parent != "." && parent != "/" {
			name = parent
		}
	}
	name = strings.TrimPrefix(name, "go-")
	if index := strings.IndexFunc(name, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_'
	}); index >= 0 {
		name = name[:index]
	}
	return name
}
