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

package directive

import (
	"fmt"
	"go/token"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// importPathSymbols holds the punctuation an import path segment may carry besides
	// letters and digits, as the go command allows.
	importPathSymbols = "-._~+"

	// maxGoTypeLength is the most characters a go_type value may hold, its import paths and
	// type arguments included; a longer value is refused before it is parsed.
	maxGoTypeLength = 1024
)

// ParseGoTypeOverride splits a go_type value into its package path and type name, and
// checks the result against what the generator can write.
//
// The value is a type name, optionally qualified by the import path of its package,
// optionally preceded by pointer and slice markers and optionally followed by type
// arguments in brackets, each written the same way: "string", "[]byte", "time.Time",
// "*example.com/menu.Pence", "github.com/x/opt.Option[uuid.UUID]". The markers stay on
// the name, where the generator expects them, and a name with type arguments becomes an
// instance of the generic type. A value longer than maxGoTypeLength characters, or whose
// type arguments nest more than sqlmodel.MaxGoTypeArgumentDepth levels deep, is refused.
//
// Takes raw (string) which is the directive value as written.
//
// Returns *sqlmodel.GoType which is the parsed override, or nil when raw is not a type
// the generator can write.
// Returns string which describes the problem when raw is unusable, and is empty
// otherwise.
func ParseGoTypeOverride(raw string) (*sqlmodel.GoType, string) {
	if longerThan(raw, maxGoTypeLength) {
		return nil, tooLongProblem()
	}
	goType, problem := splitWrittenType(raw)
	if problem == "" {
		problem = checkGoTypeShape(goType, 0)
	}
	if problem != "" {
		return nil, problem
	}
	return new(goast.WrittenGoType(goType.Package, goType.Name)), ""
}

// CheckGoTypeOverride checks a Go type already split into its package path and name, as
// the library's type overrides carry it, against what the generator can write.
//
// Takes goType (sqlmodel.GoType) which is the Go type to check.
//
// Returns string which describes the problem when the type is unusable, and is empty
// otherwise.
func CheckGoTypeOverride(goType sqlmodel.GoType) string {
	if longerThan(displayGoType(goType), maxGoTypeLength) {
		return tooLongProblem()
	}
	return checkGoTypeShape(goType, 0)
}

// IsImportPath reports whether a package path is a well-formed import path: slash
// separated segments, none empty, "." or "..", made of letters, digits and the
// punctuation the go command allows.
//
// Takes packagePath (string) which is the path to check.
//
// Returns bool which is true when the path is well formed.
func IsImportPath(packagePath string) bool {
	for segment := range strings.SplitSeq(packagePath, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, character := range segment {
			if !isImportPathCharacter(character) {
				return false
			}
		}
	}
	return true
}

// splitWrittenType splits a whole Go type as written into its package path and name, as
// goast.SplitWrittenType does, reporting a qualified type that names no package.
//
// Takes raw (string) which is the type as written.
//
// Returns sqlmodel.GoType which holds the package path and the name.
// Returns string which describes the problem, or is empty.
func splitWrittenType(raw string) (sqlmodel.GoType, string) {
	spec := strings.TrimSpace(raw)
	packagePath, name := goast.SplitWrittenType(spec)
	if packagePath == "" && name != spec {
		return sqlmodel.GoType{}, fmt.Sprintf("%q names no package before the type name", spec)
	}
	return sqlmodel.GoType{Composite: nil, Package: packagePath, Name: name}, ""
}

// unusableGoTypeDiagnostic reports a query-level column override whose go_type the
// generator cannot use, as a Q049 error on the value.
//
// Takes override (*ColumnOverride) which holds the go_type value and its span.
// Takes problem (string) which describes why the go_type is unusable.
// Takes errorBuilder (diagbuild.ErrorBuilder) which builds source-mapped diagnostics.
//
// Returns diag.SourceError which is the Q049 diagnostic.
func unusableGoTypeDiagnostic(override *ColumnOverride, problem string, errorBuilder diagbuild.ErrorBuilder) diag.SourceError {
	return errorBuilder.At(
		override.GoTypeSpan,
		diag.CodeUnusableGoTypeOverride,
		fmt.Sprintf("go_type %q for column %q is unusable: %s", sqllex.Excerpt(override.GoType, maxGoTypeLength), override.Name, problem),
	)
}

// checkGoTypeShape reports why the generator cannot write a Go type.
//
// The generator writes the name as an identifier after any pointer and slice markers,
// qualified by the name the package's import path implies when there is a package, so the
// name must be an identifier, exported when qualified, and the package path must be a
// valid import path. Any valid import path implies a name the generated code can use,
// importing the package under it when its last element is not an identifier. Each type
// argument of an instance must be a type the generator can write in turn, nested no more
// than sqlmodel.MaxGoTypeArgumentDepth levels deep.
//
// Takes goType (sqlmodel.GoType) which is the Go type to check.
// Takes depth (int) which is how deeply the type is nested as a type argument, zero for
// the whole type.
//
// Returns string which describes the problem, or is empty when the type is usable.
func checkGoTypeShape(goType sqlmodel.GoType, depth int) string {
	if depth > sqlmodel.MaxGoTypeArgumentDepth {
		return fmt.Sprintf("its type arguments nest more than %d levels deep", sqlmodel.MaxGoTypeArgumentDepth)
	}
	_, name := goast.CutTypeMarkers(goType.Name)
	generic, arguments, instance := goast.CutTypeArguments(name)
	if instance {
		name = generic
	}
	if name == "" {
		return fmt.Sprintf("%q names no type", displayGoType(goType))
	}
	if !token.IsIdentifier(name) {
		return fmt.Sprintf(
			"%q is not a Go type name; name a defined type such as string, time.Time or example.com/pkg.Type",
			name,
		)
	}
	if goType.Package != "" && !IsImportPath(goType.Package) {
		return fmt.Sprintf("%q is not a valid import path", goType.Package)
	}
	if goType.Package != "" && !token.IsExported(name) {
		return fmt.Sprintf("%q is not exported, so the generated code cannot refer to it in %q", name, goType.Package)
	}
	for _, argument := range arguments {
		argumentType, problem := splitWrittenType(argument)
		if problem == "" {
			problem = checkGoTypeShape(argumentType, depth+1)
		}
		if problem != "" {
			return problem
		}
	}
	return ""
}

// isImportPathCharacter reports whether a character may appear in an import path segment.
//
// Takes character (rune) which is the character to check.
//
// Returns bool which is true for an ASCII letter or digit or allowed punctuation.
func isImportPathCharacter(character rune) bool {
	switch {
	case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9':
		return true
	default:
		return strings.ContainsRune(importPathSymbols, character)
	}
}

// displayGoType renders a Go type as a go_type value would spell it.
//
// Takes goType (sqlmodel.GoType) which is the Go type to render.
//
// Returns string which is the package path and name joined by a dot, or the name alone.
func displayGoType(goType sqlmodel.GoType) string {
	if goType.Package == "" {
		return goType.Name
	}
	return goType.Package + "." + goType.Name
}

// longerThan reports whether a text holds more than a number of characters, reading no
// further than the character past the limit.
//
// Takes text (string) which is the text to measure.
// Takes limit (int) which is the most characters allowed.
//
// Returns bool which is true when the text holds more.
func longerThan(text string, limit int) bool {
	if len(text) <= limit {
		return false
	}
	count := 0
	for range text {
		count++
		if count > limit {
			return true
		}
	}
	return false
}

// tooLongProblem describes a go_type value longer than maxGoTypeLength characters.
//
// Returns string which is the problem.
func tooLongProblem() string {
	return fmt.Sprintf("it is longer than the %d characters a go_type may hold", maxGoTypeLength)
}
