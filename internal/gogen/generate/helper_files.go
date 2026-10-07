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

package generate

import (
	"cmp"
	"context"
	"go/scanner"
	"go/token"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/dynamic"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/sqlmodel"
)

// queryNames gathers the entries of the query-names lookup.
type queryNames struct {
	// seenExact holds the SQL texts an exact entry already names.
	seenExact map[string]bool

	// seenPrefixes holds the prefixes an entry already names.
	seenPrefixes map[string]bool

	// exact holds the entries naming a constant's whole SQL.
	exact []helpers.QueryName

	// prefixes holds the entries naming SQL by the prefix it starts with.
	prefixes []helpers.QueryNamePrefix
}

// add names a constant's SQL, and the SQL edited from it at run time when the generated
// code edits it.
//
// Takes constant (string) which is the constant.
// Takes label (string) which names the query.
// Takes rendered (*emitkit.Rendered) which holds the constant's SQL.
// Takes edit (int) which is the offset of its first run-time edit, or -1 for none.
func (names *queryNames) add(constant, label string, rendered *emitkit.Rendered, edit int) {
	if !names.seenExact[rendered.SQL] {
		names.seenExact[rendered.SQL] = true
		names.exact = append(names.exact, helpers.QueryName{Constant: constant, Name: label})
	}
	if edit <= 0 {
		return
	}
	if prefix := rendered.SQL[:edit]; !names.seenPrefixes[prefix] {
		names.seenPrefixes[prefix] = true
		names.prefixes = append(names.prefixes, helpers.QueryNamePrefix{Constant: constant, Length: edit, Name: label})
	}
}

// sharedHelperFiles renders error-labelling, sorting, bind-limit, slice-list and runtime
// builder helpers.
//
// The error-labelling helpers are included when the style asks for labelled errors, and
// the bind-limit helper whenever slice-list or runtime builder helpers need it.
//
// Takes emission (*gogen.Emission) which is the package's generation.
//
// Returns []sqlmodel.GeneratedFile which holds the helper files.
// Returns error when a query cannot be rendered, a helper cannot be, or the context is
// done.
func sharedHelperFiles(ctx context.Context, emission *gogen.Emission) ([]sqlmodel.GeneratedFile, error) {
	expandsLists, err := expandsAnyList(ctx, emission)
	if err != nil {
		return nil, err
	}
	usesRuntimeBuilder := dynamic.UsesRuntimeBuilder(emission.Request.Queries)
	packageName, vocabulary := emission.Request.Package, emission.Vocabulary
	files, err := styleHelperFiles(emission)
	if err != nil {
		return nil, err
	}
	if !expandsLists && !usesRuntimeBuilder {
		return files, nil
	}

	bindLimits, err := helpers.BindLimits(packageName, vocabulary, methods.MaxBindVariables(emission))
	if err != nil {
		return nil, err
	}
	files = append(files, bindLimits)

	if expandsLists {
		sliceHelpers, sliceErr := helpers.SliceHelpers(packageName, vocabulary, emission.BindForm())
		if sliceErr != nil {
			return nil, sliceErr
		}
		files = append(files, sliceHelpers)
	}

	if usesRuntimeBuilder {
		runtimeHelpers, runtimeErr := helpers.RuntimeHelpers(packageName, vocabulary, dynamic.RuntimeSyntax(emission))
		if runtimeErr != nil {
			return nil, runtimeErr
		}
		files = append(files, runtimeHelpers)
	}
	return files, nil
}

// styleHelperFiles renders the helpers a package needs whatever its queries bind: the
// labelling of errors, when the style asks for it, and the sort direction type, when a
// query is sortable.
//
// Takes emission (*gogen.Emission) which is the package's generation.
//
// Returns []sqlmodel.GeneratedFile which holds the helper files, or none.
// Returns error when a helper cannot be rendered.
func styleHelperFiles(emission *gogen.Emission) ([]sqlmodel.GeneratedFile, error) {
	packageName, vocabulary := emission.Request.Package, emission.Vocabulary
	var files []sqlmodel.GeneratedFile
	if vocabulary.WrapsErrors() {
		wrapping, err := helpers.ErrorWrapping(packageName, vocabulary)
		if err != nil {
			return nil, err
		}
		files = append(files, wrapping)
	}
	if dynamic.UsesSortable(emission.Request.Queries) {
		sortable, err := helpers.Sortable(packageName, vocabulary)
		if err != nil {
			return nil, err
		}
		files = append(files, sortable)
	}
	return files, nil
}

// expandsAnyList reports whether any of a package's queries splices a slice's list into
// its SQL at run time.
//
// Takes emission (*gogen.Emission) which is the package's generation.
//
// Returns bool which is true when a query expands a list.
// Returns error when a query cannot be rendered or the context is done.
func expandsAnyList(ctx context.Context, emission *gogen.Emission) (bool, error) {
	for _, analysed := range emission.Request.Queries {
		if err := stopped(ctx, "shared helpers for "+emission.Request.Package); err != nil {
			return false, err
		}
		query, err := emission.Query(analysed, goast.NewImportTracker())
		if err != nil {
			return false, err
		}
		if methods.MethodArguments(query).Expands() {
			return true, nil
		}
	}
	return false, nil
}

// queryNamesFile renders the query-names file: the lookup that names the query any SQL
// the package runs came from, for labelling telemetry.
//
// The lookup holds every query's SQL constant, and a runtime builder's count constant,
// mapped to the query's label, in the naming plan's order; a constant whose SQL another
// earlier constant already holds is left out. SQL the generated code edits at run time,
// writing a slice's list, a sortable ORDER BY, a runtime builder's additions or the rows
// of a batch whose driver repeats the VALUES row, is named by the longest prefix it
// starts with: each such constant up to its first edit, so the lookup holds a prefix only
// where the generated code reads one. A copyfrom query whose method runs no SQL text,
// streaming its rows through a copy protocol, is left out.
//
// Takes emission (*gogen.Emission) which is the package's generation.
//
// Returns sqlmodel.GeneratedFile which contains the query-names file.
// Returns error when a query cannot be rendered, the file cannot be, or the context is
// done.
func queryNamesFile(ctx context.Context, emission *gogen.Emission) (sqlmodel.GeneratedFile, error) {
	names := &queryNames{exact: nil, prefixes: nil, seenExact: make(map[string]bool), seenPrefixes: make(map[string]bool)}
	for _, analysed := range gogen.PlanOrder(emission.Request.Queries) {
		if err := stopped(ctx, "query names for "+emission.Request.Package); err != nil {
			return sqlmodel.GeneratedFile{Name: "", Content: nil}, err
		}
		query, err := emission.Query(analysed, goast.NewImportTracker())
		if err != nil {
			return sqlmodel.GeneratedFile{Name: "", Content: nil}, err
		}
		runsSQL, repeatsRow := gogen.StatementRun(emission.Driver, query)
		if !runsSQL {
			continue
		}
		names.add(query.Naming.SQLConstant, query.Naming.Label, &query.Rendered, gogen.FirstEdit(query, &query.Rendered, repeatsRow))
		if query.Count != nil {
			names.add(query.Naming.CountConstant, query.Naming.Label, query.Count, gogen.FirstEdit(query, query.Count, repeatsRow))
		}
	}
	slices.SortStableFunc(names.prefixes, func(left, right helpers.QueryNamePrefix) int {
		return cmp.Compare(right.Length, left.Length)
	})
	style := emission.Request.Style.QueryNames
	return helpers.QueryNames(emission.Request.Package, emission.Vocabulary, style.File, style.Function, names.exact, names.prefixes)
}

// inlineJSONFile renders the inline JSON helper file when the generated code refers to
// the inline JSON type or the inline scan adapter, and returns a file with empty content
// when it refers to neither.
//
// A reference is an unqualified identifier with the type's or adapter's name, so a
// qualified name such as pkg.JSON does not count, and neither does a model or named type
// the naming plan called JSON, since the plan gives the name to the inline type only when
// the package uses it. A struct field that happens to be called JSON does count, which at
// worst emits an unused type.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes referenced (map[string]struct{}) which holds the unqualified identifiers of the
// files generated so far.
//
// Returns sqlmodel.GeneratedFile which is the helper file, or one with empty content.
// Returns error when the helper cannot be rendered or the context is done.
func inlineJSONFile(ctx context.Context, emission *gogen.Emission, referenced map[string]struct{}) (sqlmodel.GeneratedFile, error) {
	if err := stopped(ctx, "inline JSON helpers for "+emission.Request.Package); err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, err
	}
	_, needsType := referenced[emit.InlineJSONTypeName]
	needsType = needsType && !declaredByPlan(&emission.Request.Naming, emit.InlineJSONTypeName)
	_, needsScanInto := referenced[emission.Vocabulary.InlineScanInto()]
	if !needsType && !needsScanInto {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, nil
	}
	return helpers.InlineJSON(emission.Request.Package, emission.Vocabulary, needsScanInto, needsType)
}

// convertStringsFile renders the string conversion helpers used by a package.
//
// The returned file has empty content when no conversions are used.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes referenced (map[string]struct{}) which holds the unqualified identifiers of the
// files generated so far.
//
// Returns sqlmodel.GeneratedFile which is the helper file, or one with empty content.
// Returns error when the helpers cannot be rendered or the context is done.
func convertStringsFile(ctx context.Context, emission *gogen.Emission, referenced map[string]struct{}) (sqlmodel.GeneratedFile, error) {
	if err := stopped(ctx, "string conversions for "+emission.Request.Package); err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, err
	}
	_, convertsSlices := referenced[emission.Vocabulary.Helper(gogen.SuffixConvertStrings)]
	_, convertsPointers := referenced[emission.Vocabulary.Helper(gogen.SuffixConvertStringPointers)]
	_, convertsMaps := referenced[emission.Vocabulary.Helper(gogen.SuffixConvertStringMap)]
	_, dereferences := referenced[emission.Vocabulary.Helper(gogen.SuffixDereference)]
	if !convertsSlices && !convertsPointers && !convertsMaps {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, nil
	}
	conversions := helpers.StringConversions{
		Slices: convertsSlices, Pointers: convertsPointers, Maps: convertsMaps, Dereferences: dereferences,
	}
	return helpers.ConvertStrings(emission.Request.Package, emission.Vocabulary, conversions)
}

// declaredByPlan reports whether the naming plan gives a name to a model or a named type
// the package declares, in which case a reference to the name is to that type, and the
// inline JSON type, which the plan then left unused, is not needed.
//
// Takes naming (*emit.Naming) which is the plan.
// Takes name (string) which is the name.
//
// Returns bool which is true when a model or named type has the name.
func declaredByPlan(naming *emit.Naming, name string) bool {
	for index := range naming.Models {
		if naming.Models[index].Type == name {
			return true
		}
	}
	for index := range naming.Types {
		if naming.Types[index].Name == name {
			return true
		}
	}
	return false
}

// unqualifiedIdentifiers scans every Go file and collects the identifiers that are not
// preceded by a dot, which covers every reference to a package-level declaration of the
// generated package.
//
// Takes files ([]sqlmodel.GeneratedFile) which are the generated files.
//
// Returns map[string]struct{} which holds the unqualified identifier names.
func unqualifiedIdentifiers(files []sqlmodel.GeneratedFile) map[string]struct{} {
	names := make(map[string]struct{})
	fileSet := token.NewFileSet()
	for _, file := range files {
		if !strings.HasSuffix(file.Name, ".go") {
			continue
		}
		var tokens scanner.Scanner
		tokens.Init(fileSet.AddFile(file.Name, -1, len(file.Content)), file.Content, nil, 0)
		previous := token.ILLEGAL
		for {
			_, kind, literal := tokens.Scan()
			if kind == token.EOF {
				break
			}
			if kind == token.IDENT && previous != token.PERIOD {
				names[literal] = struct{}{}
			}
			previous = kind
		}
	}
	return names
}
