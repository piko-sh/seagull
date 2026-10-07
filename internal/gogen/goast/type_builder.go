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

package goast

import (
	"go/ast"
	"go/token"
	"maps"
	"path"
	"strconv"
	"strings"
	"unicode"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// defaultImportQualifier is the qualifier of an import path that implies no package name
	// at all.
	defaultImportQualifier = "pkg"
)

// ImportTracker collects import paths required by the generated file and provides AST
// expression builders for Go types.
//
// When two import paths share the same path.Base (e.g. github.com/google/uuid and
// myapp/uuid both reduce to "uuid"), the second one is allocated a numeric-suffixed alias
// (uuid2) so the generated code compiles.
type ImportTracker struct {
	// imports maps import paths to the alias used in the generated file. The alias is empty
	// when the path's natural package name (path.Base) is unambiguous; non-empty when a
	// collision-driven alias was allocated.
	imports map[string]string

	// aliases tracks the reverse: which path currently owns each alias string. Used to
	// detect collisions on subsequent calls.
	aliases map[string]string

	// plan maps import paths to the package-wide qualifier the naming plan chose for them,
	// or is nil for a tracker that chooses qualifiers per file.
	plan map[string]string
}

// NewImportTracker creates a new import tracker that chooses each import's qualifier for
// its own file: the path's last element, or the one before a version suffix, suffixed
// with a number when another path in the file already has it.
//
// Returns *ImportTracker which is ready to collect imports.
func NewImportTracker() *ImportTracker {
	return &ImportTracker{
		imports: make(map[string]string),
		aliases: make(map[string]string),
		plan:    nil,
	}
}

// NewPlannedImportTracker creates a tracker using the package's planned import
// qualifiers.
//
// Unplanned imports receive a derived qualifier with a numeric suffix when needed to
// avoid collisions.
//
// Takes plan (map[string]string) which maps import paths to their qualifiers, as
// emit.Naming.Imports holds them. The tracker does not change it.
//
// Returns *ImportTracker which is ready to collect imports.
func NewPlannedImportTracker(plan map[string]string) *ImportTracker {
	return &ImportTracker{
		imports: make(map[string]string),
		aliases: make(map[string]string),
		plan:    plan,
	}
}

// AddType converts a sqlmodel.GoType to an ast.Expr, registering any required import in
// the tracker. When the new import's natural alias (path.Base) collides with an existing
// import from a different path, a numeric-suffixed alias is allocated so the generated
// code keeps both imports unambiguous.
//
// Takes goType (sqlmodel.GoType) which specifies the Go type to represent.
//
// Returns ast.Expr which is the AST representation of the type.
func (tracker *ImportTracker) AddType(goType sqlmodel.GoType) ast.Expr {
	typeName := goType.Name

	if strings.HasPrefix(typeName, "*") {
		inner := goType
		inner.Name = typeName[1:]
		return goastutil.StarExpr(tracker.AddType(inner))
	}

	if strings.HasPrefix(typeName, "[]") {
		inner := goType
		inner.Name = typeName[2:]
		return &ast.ArrayType{Elt: tracker.AddType(inner)}
	}

	if goType.Composite != nil {
		return tracker.addComposite(goType.Composite)
	}

	if goType.Package == "" {
		return goastutil.CachedIdent(typeName)
	}

	packageAlias := tracker.resolveAlias(goType.Package)

	return &ast.SelectorExpr{
		X:   goastutil.CachedIdent(packageAlias),
		Sel: goastutil.CachedIdent(typeName),
	}
}

// ApplyImports adds the tracked imports that the file's declarations use to the given AST
// file.
//
// Aliased imports are emitted with their explicit alias name. An import the declarations
// never qualify is left out, because generated code must compile without a later
// goimports pass removing it.
//
// Takes fileSet (*token.FileSet) which holds position information.
// Takes file (*ast.File) which is the file to add imports to.
func (tracker *ImportTracker) ApplyImports(fileSet *token.FileSet, file *ast.File) {
	used := referencedQualifiers(file)
	for importPath, alias := range tracker.imports {
		if !importReferenced(used, importPath, alias) {
			continue
		}
		if alias == "" {
			goastutil.AddImport(fileSet, file, importPath)
			continue
		}
		goastutil.AddNamedImport(fileSet, file, alias, importPath)
	}
}

// Qualifiers returns the qualifier of each import the tracker registered, mapped to the
// import's path.
//
// Returns map[string]string which maps each qualifier to its path; the caller may change
// it.
func (tracker *ImportTracker) Qualifiers() map[string]string {
	return maps.Clone(tracker.aliases)
}

// AddImport registers an import path without an associated type.
//
// Takes importPath (string) which is the import path to add.
func (tracker *ImportTracker) AddImport(importPath string) {
	tracker.resolveAlias(importPath)
}

// addComposite builds the expression of a map or struct type, registering the imports its
// parts need.
//
// Takes composite (*sqlmodel.GoComposite) which describes the type.
//
// Returns ast.Expr which is the map or struct type expression.
func (tracker *ImportTracker) addComposite(composite *sqlmodel.GoComposite) ast.Expr {
	if composite.Kind == sqlmodel.GoCompositeMap && composite.Key != nil && composite.Value != nil {
		return &ast.MapType{Key: tracker.AddType(*composite.Key), Value: tracker.AddType(*composite.Value)}
	}
	if composite.Kind == sqlmodel.GoCompositeInstance && composite.Value != nil {
		return tracker.addInstance(composite)
	}
	fields := make([]*ast.Field, 0, len(composite.Fields))
	for _, field := range composite.Fields {
		structField := &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(field.Name)},
			Type:  tracker.AddType(field.Type),
		}
		if field.Tag != "" {
			structField.Tag = &ast.BasicLit{Kind: token.STRING, Value: "`" + field.Tag + "`"}
		}
		fields = append(fields, structField)
	}
	return &ast.StructType{Fields: &ast.FieldList{List: fields}}
}

// addInstance builds the expression of an instance of a generic type, Generic[Arguments],
// registering the imports the generic type and its arguments need.
//
// Takes composite (*sqlmodel.GoComposite) which describes the instance; its Value is set.
//
// Returns ast.Expr which is the index expression.
func (tracker *ImportTracker) addInstance(composite *sqlmodel.GoComposite) ast.Expr {
	generic := tracker.AddType(*composite.Value)
	arguments := make([]ast.Expr, 0, len(composite.Arguments))
	for _, argument := range composite.Arguments {
		arguments = append(arguments, tracker.AddType(argument))
	}
	if len(arguments) == 1 {
		return &ast.IndexExpr{X: generic, Index: arguments[0]}
	}
	return &ast.IndexListExpr{X: generic, Indices: arguments}
}

// resolveAlias returns the alias used to refer to the given import path in the generated
// code, registering the import on first call.
//
// Collisions on path.Base produce numeric-suffixed aliases (uuid2, uuid3, ...).
//
// Takes importPath (string) which is the import path to resolve an alias for.
//
// Returns string which is the alias to refer to the import path by.
func (tracker *ImportTracker) resolveAlias(importPath string) string {
	if tracker.plan != nil {
		return tracker.resolvePlannedAlias(importPath)
	}
	if existing, ok := tracker.imports[importPath]; ok {
		if existing != "" {
			return existing
		}
		return naturalImportAlias(importPath)
	}

	naturalAlias := naturalImportAlias(importPath)
	owner, taken := tracker.aliases[naturalAlias]
	if !taken {
		tracker.imports[importPath] = ""
		tracker.aliases[naturalAlias] = importPath
		return naturalAlias
	}
	if owner == importPath {
		tracker.imports[importPath] = ""
		return naturalAlias
	}

	suffix := 2
	for {
		candidate := naturalAlias + strconv.Itoa(suffix)
		if _, exists := tracker.aliases[candidate]; !exists {
			tracker.imports[importPath] = candidate
			tracker.aliases[candidate] = importPath
			return candidate
		}
		suffix++
	}
}

// resolvePlannedAlias registers an import and chooses its qualifier.
//
// It uses the planned name when available, otherwise deriving a name that avoids existing
// qualifiers.
//
// Takes importPath (string) which is the import path to resolve a qualifier for.
//
// Returns string which is the qualifier.
func (tracker *ImportTracker) resolvePlannedAlias(importPath string) string {
	if alias, registered := tracker.imports[importPath]; registered {
		if alias != "" {
			return alias
		}
		return assumedPackageName(importPath)
	}
	qualifier, planned := tracker.plan[importPath]
	if !planned {
		qualifier = tracker.unplannedQualifier(importPath)
	}
	alias := ""
	if qualifier != assumedPackageName(importPath) {
		alias = qualifier
	}
	tracker.imports[importPath] = alias
	tracker.aliases[qualifier] = importPath
	return qualifier
}

// unplannedQualifier chooses the qualifier of an import path a planned tracker's plan
// does not hold: ImportQualifier's, suffixed with the first number that leaves it clear
// of the file's other imports and of every qualifier in the plan.
//
// Takes importPath (string) which is the import path.
//
// Returns string which is the qualifier.
func (tracker *ImportTracker) unplannedQualifier(importPath string) string {
	taken := make(map[string]struct{}, len(tracker.aliases)+len(tracker.plan))
	for qualifier := range tracker.aliases {
		taken[qualifier] = struct{}{}
	}
	for _, qualifier := range tracker.plan {
		taken[qualifier] = struct{}{}
	}
	return goastutil.DisambiguateIdentifier(ImportQualifier(importPath), taken)
}

// ImportQualifier derives a usable Go qualifier from an import path's assumed package
// name. It uses pkg when the path supplies no name.
//
// Takes importPath (string) which is the import path.
//
// Returns string which is the qualifier.
func ImportQualifier(importPath string) string {
	assumed := assumedPackageName(importPath)
	if assumed == "" {
		return defaultImportQualifier
	}
	return goastutil.SanitiseGoIdentifier(assumed)
}

// AssumedPackageName derives a package identifier from its import path.
//
// Version suffixes and go- prefixes are removed, and the name stops at the first invalid
// identifier character. This follows goimports' assumptions.
//
// Takes importPath (string) which is the import path.
//
// Returns string which is the implied package name, empty when the path implies none.
func AssumedPackageName(importPath string) string {
	return assumedPackageName(importPath)
}

// PackageQualifier returns the identifier generated code uses to refer to the package at
// importPath when no collision forces an alias, honouring the semantic-import-versioning
// convention.
//
// Takes importPath (string) which is the full import path.
//
// Returns string which is the package qualifier.
func PackageQualifier(importPath string) string {
	return naturalImportAlias(importPath)
}

// ResolveGoType maps a SQL type to its corresponding Go type using the mapping table.
//
// Takes sqlType (sqlmodel.SQLType) which is the SQL type to map.
// Takes nullable (bool) which indicates whether the column permits NULL.
// Takes mappings (sqlmodel.GoTypeResolver) which defines the mapping rules.
//
// Returns sqlmodel.GoType which is the resolved Go type.
func ResolveGoType(
	sqlType sqlmodel.SQLType,
	nullable bool,
	mappings sqlmodel.GoTypeResolver,
) sqlmodel.GoType {
	return mappings.Resolve(sqlType, nullable)
}

// importReferenced reports whether the file refers to an import.
//
// An aliased import is referred to by its alias. An unaliased one is referred to by its
// package name, which generated code takes to be the name goimports assumes from the path
// (clickhouse for github.com/ClickHouse/clickhouse-go/v2), or the path's last element.
//
// Takes used (map[string]struct{}) which holds the qualifiers the file uses.
// Takes importPath (string) which is the import path to check.
// Takes alias (string) which is the import's explicit alias, or empty.
//
// Returns bool which is true when the file uses the import.
func importReferenced(used map[string]struct{}, importPath, alias string) bool {
	if alias != "" {
		_, referenced := used[alias]
		return referenced
	}
	if _, referenced := used[assumedPackageName(importPath)]; referenced {
		return true
	}
	_, referenced := used[naturalImportAlias(importPath)]
	return referenced
}

// assumedPackageName returns the package name goimports assumes for an import path: the
// last element, or the one before a version suffix such as v2, without a "go-" prefix and
// cut at the first character that cannot appear in an identifier.
//
// Takes importPath (string) which is the import path.
//
// Returns string which is the assumed package name.
func assumedPackageName(importPath string) string {
	base := naturalImportAlias(importPath)
	base = strings.TrimPrefix(base, "go-")
	if index := strings.IndexFunc(base, notIdentifierRune); index >= 0 {
		base = base[:index]
	}
	return base
}

// notIdentifierRune reports whether a rune cannot appear in a Go identifier.
//
// Takes character (rune) which is the rune to test.
//
// Returns bool which is true for runes outside letters, digits and underscore.
func notIdentifierRune(character rune) bool {
	return !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_'
}

// referencedQualifiers collects every identifier the file uses as the left-hand side of a
// selector, such as "sql" in sql.ErrNoRows, which is how generated code refers to an
// imported package.
//
// Takes file (*ast.File) which is the file to scan.
//
// Returns map[string]struct{} which holds the qualifier names found.
func referencedQualifiers(file *ast.File) map[string]struct{} {
	used := make(map[string]struct{})
	for qualifier := range SelectorQualifiers(file) {
		used[qualifier] = struct{}{}
	}
	return used
}

// naturalImportAlias returns the package alias for an import path, accounting for Go's
// semantic-import-versioning convention: the package for `github.com/foo/bar/v2` is
// `bar`, not `v2`. When the final path segment is a version marker (vN), the preceding
// segment is used; otherwise path.Base is the natural alias.
//
// Takes importPath (string) which is the full import path.
//
// Returns string which is the package alias.
func naturalImportAlias(importPath string) string {
	base := path.Base(importPath)
	if isVersionSegment(base) {
		if parent := path.Base(path.Dir(importPath)); parent != "" && parent != "." && parent != "/" {
			return parent
		}
	}
	return base
}

// isVersionSegment reports whether segment is a semantic-import-versioning marker such as
// v2, v3, ... (a leading 'v' followed by one or more digits).
//
// Takes segment (string) which is the path segment to test.
//
// Returns bool which is true when the segment is a version marker.
func isVersionSegment(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for index := 1; index < len(segment); index++ {
		if segment[index] < '0' || segment[index] > '9' {
			return false
		}
	}
	return true
}
