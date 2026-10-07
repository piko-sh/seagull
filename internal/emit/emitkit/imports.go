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
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// pointerModifier makes a Go type a pointer to the type after it.
	pointerModifier = "*"

	// sliceModifier makes a Go type a slice of the type after it.
	sliceModifier = "[]"
)

// FileImports collects and writes a generated file's imports using planned qualifiers.
//
// Emitters writing source text use it for type and package references. It is not safe for
// concurrent use.
type FileImports struct {
	// planned maps each import path the plan holds to its qualifier.
	planned map[string]string

	// used maps each import path the file refers to to the qualifier it uses.
	used map[string]string
}

// NewFileImports returns the imports of one file of a package planned with a naming plan,
// which refers to no package yet.
//
// Takes naming (*emit.Naming) which is the package's naming plan, whose Imports gives
// each package its qualifier. NewFileImports does not change it.
//
// Returns *FileImports which collects the file's imports.
func NewFileImports(naming *emit.Naming) *FileImports {
	return &FileImports{planned: maps.Clone(naming.Imports), used: make(map[string]string)}
}

// Qualifier records an import and returns the qualifier used to reference it.
//
// It uses the package's naming plan when available. An import the plan does not hold
// takes a name derived from its path, as the generator's import planner names it:
// numbered past every qualifier the plan holds and every one the file already uses, so it
// never meets another package's.
//
// Takes importPath (string) which is the import path, such as database/sql.
//
// Returns string which is the qualifier, such as sql.
func (imports *FileImports) Qualifier(importPath string) string {
	if qualifier, recorded := imports.used[importPath]; recorded {
		return qualifier
	}
	qualifier, planned := imports.planned[importPath]
	if !planned {
		qualifier = imports.unplannedQualifier(importPath)
	}
	imports.used[importPath] = qualifier
	return qualifier
}

// Type returns a Go type as Go source, such as *decimal.Decimal, []string, map[string]any
// or a struct type, referring to each package it names by its qualifier and recording the
// import.
//
// A sqlmodel.GoType writes a pointer or slice of a type as the prefix of its Name, which
// Type writes before the qualified name.
//
// Takes goType (sqlmodel.GoType) which is the type, as the type mapping or a
// GoTypeOverride gives it.
//
// Returns string which is the type's Go source.
func (imports *FileImports) Type(goType sqlmodel.GoType) string {
	modifiers, name := splitModifiers(goType.Name)
	switch {
	case goType.Composite != nil:
		return modifiers + imports.composite(goType.Composite)
	case goType.Package == "":
		return modifiers + name
	default:
		return modifiers + imports.Qualifier(goType.Package) + "." + name
	}
}

// Declaration returns the file's import declaration, its paths sorted, each with an alias
// where its qualifier differs from the name the path implies, or the empty string when
// the file imports nothing.
//
// Returns string which is the declaration, ending in a newline.
func (imports *FileImports) Declaration() string {
	if len(imports.used) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("import (\n")
	for _, importPath := range slices.Sorted(maps.Keys(imports.used)) {
		builder.WriteString("\t")
		if qualifier := imports.used[importPath]; qualifier != goast.AssumedPackageName(importPath) {
			builder.WriteString(qualifier + " ")
		}
		builder.WriteString(`"` + importPath + `"` + "\n")
	}
	builder.WriteString(")\n")
	return builder.String()
}

// composite writes a map or struct type, or an instance of a generic type, as Go source.
//
// Takes composite (*sqlmodel.GoComposite) which describes the type.
//
// Returns string which is the type's Go source.
func (imports *FileImports) composite(composite *sqlmodel.GoComposite) string {
	if composite.Kind == sqlmodel.GoCompositeMap && composite.Key != nil && composite.Value != nil {
		return "map[" + imports.Type(*composite.Key) + "]" + imports.Type(*composite.Value)
	}
	if composite.Kind == sqlmodel.GoCompositeInstance && composite.Value != nil {
		arguments := make([]string, 0, len(composite.Arguments))
		for _, argument := range composite.Arguments {
			arguments = append(arguments, imports.Type(argument))
		}
		return imports.Type(*composite.Value) + "[" + strings.Join(arguments, ", ") + "]"
	}
	var builder strings.Builder
	builder.WriteString("struct {")
	for index, field := range composite.Fields {
		if index > 0 {
			builder.WriteString(";")
		}
		builder.WriteString(" " + field.Name + " " + imports.Type(field.Type))
		if field.Tag != "" {
			builder.WriteString(" `" + field.Tag + "`")
		}
	}
	builder.WriteString(" }")
	return builder.String()
}

// unplannedQualifier chooses the qualifier of an import path the plan does not hold: the
// name its path implies, numbered past every qualifier the plan holds and the file uses.
//
// Takes importPath (string) which is the import path.
//
// Returns string which is the qualifier.
func (imports *FileImports) unplannedQualifier(importPath string) string {
	taken := make(map[string]struct{}, len(imports.planned)+len(imports.used))
	for _, qualifier := range imports.planned {
		taken[qualifier] = struct{}{}
	}
	for _, qualifier := range imports.used {
		taken[qualifier] = struct{}{}
	}
	return goastutil.DisambiguateIdentifier(goast.ImportQualifier(importPath), taken)
}

// splitModifiers splits a GoType name into the pointer and slice modifiers it starts with
// and the type name after them.
//
// Takes name (string) which is the name, such as []*Decimal.
//
// Returns modifiers (string) which holds the modifiers, such as []*.
// Returns base (string) which is the name after them, such as Decimal.
func splitModifiers(name string) (modifiers, base string) {
	base = name
	for {
		switch {
		case strings.HasPrefix(base, pointerModifier):
			base = base[len(pointerModifier):]
		case strings.HasPrefix(base, sliceModifier):
			base = base[len(sliceModifier):]
		default:
			return name[:len(name)-len(base)], base
		}
	}
}
