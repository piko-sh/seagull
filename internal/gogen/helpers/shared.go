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

package helpers

import (
	"embed"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// bindLimitsFile is the generated file holding the bind limit.
	bindLimitsFile = "bind_limits.go"

	// sliceHelpersFile is the generated file holding the slice expander.
	sliceHelpersFile = "slice_helpers.go"

	// runtimeHelpersFile is the generated file holding the runtime builder's helpers.
	runtimeHelpersFile = "runtime_helpers.go"

	// sortableFile is the generated file holding the sort direction type.
	sortableFile = "sortable.go"

	// ConvertStringsFile is the generated file holding the conversions of a slice, a slice
	// of pointers and a map's values from one string type to another.
	ConvertStringsFile = "convert_strings.go"

	// ErrorWrappingFile is the generated file holding the helpers that label the errors
	// generated methods return.
	ErrorWrappingFile = "error_wrapping.go"

	// queryNamesSource is the source of the query-names lookup.
	queryNamesSource = "query_names.go"

	// queryNamesFunction is the name the query-names source declares its lookup with.
	queryNamesFunction = "QueryName"

	// jsonSource is the source of the inline JSON type and scan adapter.
	jsonSource = "json.go"

	// jsonFileSuffix completes the name of the inline JSON helper file after the helper
	// prefix.
	jsonFileSuffix = "_json.go"
)

// sources holds the shared helpers' sources.
//
//go:embed source/bind_limits.go source/slice_helpers.go source/runtime_helpers.go
//go:embed source/json.go source/query_names.go source/sortable.go
//go:embed source/convert_strings.go source/error_wrapping.go
var sources embed.FS

// preparedSources caches immutable parsed helper templates for this emitter.
var preparedSources = NewSources(sources, "source")

// RuntimeSyntax is what a runtime builder's helpers write and accept for one dialect.
type RuntimeSyntax struct {
	// ColumnExpression is the regular expression a filtered or sorted column expression must
	// match.
	ColumnExpression string

	// Lists holds the tests written for IN and NOT IN an empty list.
	Lists dialect.ListSyntax

	// Pagination says how a limit and an offset are written.
	Pagination dialect.PaginationSyntax

	// Form is the numbered or anonymous form every bind placeholder is written in.
	Form dialect.PlaceholderSigil

	// Operators lists every predicate operator the allow-list accepts, in order.
	Operators []string

	// JSONPathOpenings lists, in lower case and with the opening parenthesis, every function
	// that reads a path from a JSON column, such as json_extract(.
	JSONPathOpenings []string

	// RootOperators lists the JSON path operators whose first occurrence ends a column's
	// root identifier.
	RootOperators []string

	// CastPatterns holds, for each form a column may be converted in, the regular expression
	// matching it, capturing the column and the type.
	CastPatterns []string

	// Casts lists the forms a column may be converted in, in the order of CastPatterns.
	Casts []dialect.CastForm

	// CastTypes lists the types a column may be converted to, as the dialect spells them.
	CastTypes []string
}

// QueryName is one entry of the query-names lookup naming a constant's whole SQL.
type QueryName struct {
	// Constant is the identifier of the constant holding the SQL.
	Constant string

	// Name is the query's name.
	Name string
}

// QueryNamePrefix is one entry of the query-names lookup naming the SQL the generated
// code edits from a constant at run time, by the constant's text before its first edit.
type QueryNamePrefix struct {
	// Constant is the identifier of the constant holding the SQL.
	Constant string

	// Name is the query's name.
	Name string

	// Length is the number of bytes of the constant before its first edit.
	Length int
}

// StringConversions says which conversions between string types convert_strings.go holds,
// each rendered only when the generated code calls it.
type StringConversions struct {
	// Slices includes the conversion of a slice, sglConvertStrings.
	Slices bool

	// Pointers includes the conversion of a slice of pointers, sglConvertStringPointers.
	Pointers bool

	// Maps includes the conversion of a map's values, sglConvertStringMap.
	Maps bool

	// Dereferences includes the function that reads an optional parameter through its
	// pointer before it is converted, sglDereference.
	Dereferences bool
}

// BindLimits renders bind_limits.go: the most parameters one statement binds and the
// error returned past it, which the slice expander and the runtime builder share.
//
// Takes packageName (string) which is the generated package's name.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes maxBindVariables (int) which is the most parameters one statement binds.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func BindLimits(packageName string, vocabulary Vocabulary, maxBindVariables int) (sqlmodel.GeneratedFile, error) {
	return renderEmbedded(bindLimitsFile, vocabulary, &Data{
		Flags:   nil,
		Values:  map[string]any{"maxBindVariables": maxBindVariables},
		Renames: nil,
		Package: packageName,
	})
}

// ErrorWrapping renders error_wrapping.go: the helpers that label an error a generated
// method returns with the method's name, keeping the error beneath for errors.Is.
//
// Takes packageName (string) which is the generated package's name.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func ErrorWrapping(packageName string, vocabulary Vocabulary) (sqlmodel.GeneratedFile, error) {
	return renderEmbedded(ErrorWrappingFile, vocabulary, &Data{Flags: nil, Values: nil, Renames: nil, Package: packageName})
}

// SliceHelpers renders slice_helpers.go: the function that writes a slice parameter's
// list into a query at the offsets the generator marked, one placeholder per element in a
// form, or the dialect's empty test for an empty slice. The rendered function writes a
// placeholder's suffix only for a form that has one.
//
// Takes packageName (string) which is the generated package's name.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes form (dialect.PlaceholderSigil) which is the numbered or anonymous form each
// placeholder is written in.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func SliceHelpers(packageName string, vocabulary Vocabulary, form dialect.PlaceholderSigil) (sqlmodel.GeneratedFile, error) {
	return renderEmbedded(sliceHelpersFile, vocabulary, &Data{
		Flags: nil,
		Values: map[string]any{
			"prefix":   form.Prefix,
			"suffix":   form.Suffix,
			"suffixed": form.Suffix != "",
			"numbered": form.Kind == dialect.PlaceholderNumbered,
		},
		Renames: nil,
		Package: packageName,
	})
}

// RuntimeHelpers renders runtime_helpers.go: the allow-lists, validators and the
// predicate, ordering and pagination writers every runtime builder of a package shares.
//
// Takes packageName (string) which is the generated package's name.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes syntax (*RuntimeSyntax) which is what the helpers write and accept.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func RuntimeHelpers(packageName string, vocabulary Vocabulary, syntax *RuntimeSyntax) (sqlmodel.GeneratedFile, error) {
	before := make([]string, len(syntax.Casts))
	middle := make([]string, len(syntax.Casts))
	after := make([]string, len(syntax.Casts))
	for index, cast := range syntax.Casts {
		before[index], middle[index], after[index] = cast.Before, cast.Middle, cast.After
	}
	return renderEmbedded(runtimeHelpersFile, vocabulary, &Data{
		Flags: nil,
		Values: map[string]any{
			"prefix":           syntax.Form.Prefix,
			"suffix":           syntax.Form.Suffix,
			"numbered":         syntax.Form.Kind == dialect.PlaceholderNumbered,
			"offsetFirst":      syntax.Pagination.OffsetFirst,
			"jsonPathOpenings": syntax.JSONPathOpenings,
			"rootOperators":    syntax.RootOperators,
			"emptyMembership":  syntax.Lists.EmptyMembership,
			"emptyExclusion":   syntax.Lists.EmptyExclusion,
			"limitBefore":      syntax.Pagination.Limit.Before,
			"limitAfter":       syntax.Pagination.Limit.After,
			"offsetBefore":     syntax.Pagination.Offset.Before,
			"offsetAfter":      syntax.Pagination.Offset.After,
			"unboundedLimit":   syntax.Pagination.UnboundedLimit,
			"operators":        allowList(syntax.Operators),
			"columnExpression": syntax.ColumnExpression,
			"castPatterns":     regexpList(syntax.CastPatterns),
			"castBefore":       before,
			"castMiddle":       middle,
			"castAfter":        after,
			"castTypes":        syntax.CastTypes,
		},
		Renames: nil,
		Package: packageName,
	})
}

// InlineJSON renders the inline JSON helper file, named after the helper prefix, such as
// sgl_json.go: the JSON type a JSON value maps to when its slot keeps its default, and
// the scan adapter that decodes a JSON column into a typed destination.
//
// Takes packageName (string) which is the generated package's name.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes scanInto (bool) which includes the scan adapter.
// Takes jsonType (bool) which includes the JSON type.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func InlineJSON(packageName string, vocabulary Vocabulary, scanInto, jsonType bool) (sqlmodel.GeneratedFile, error) {
	return preparedSources.File(vocabulary.Helper(jsonFileSuffix), jsonSource, vocabulary, &Data{
		Flags:   map[string]bool{"scanInto": scanInto, "jsonType": jsonType},
		Values:  nil,
		Renames: nil,
		Package: packageName,
	})
}

// QueryNames renders exact and prefix SQL lookups for telemetry query names.
//
// Prefixes are checked longest first; packages without edited SQL use exact lookup alone.
//
// Takes packageName (string) which is the generated package's name.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes file (string) which is the generated file's name.
// Takes function (string) which is the lookup function's name.
// Takes exact ([]QueryName) which are the map's entries, in order.
// Takes prefixes ([]QueryNamePrefix) which are the prefix entries, in the order they are
// tried.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func QueryNames(packageName string, vocabulary Vocabulary, file, function string, exact []QueryName, prefixes []QueryNamePrefix) (sqlmodel.GeneratedFile, error) {
	pairs := make([]string, len(exact))
	for index, entry := range exact {
		pairs[index] = entry.Constant + ": " + strconv.Quote(entry.Name)
	}
	prefixTexts := make([]string, len(prefixes))
	names := make([]string, len(prefixes))
	for index, entry := range prefixes {
		prefixTexts[index] = entry.Constant + "[:" + strconv.Itoa(entry.Length) + "]"
		names[index] = strconv.Quote(entry.Name)
	}
	return preparedSources.File(file, queryNamesSource, vocabulary, &Data{
		Flags: map[string]bool{"prefixed": len(prefixes) > 0},
		Values: map[string]any{
			"prefixed":    len(prefixes) > 0,
			"names":       Code{Text: "map[string]string{" + strings.Join(pairs, ", ") + "}", Imports: nil},
			"prefixes":    Code{Text: "[]string{" + strings.Join(prefixTexts, ", ") + "}", Imports: nil},
			"prefixNames": Code{Text: "[]string{" + strings.Join(names, ", ") + "}", Imports: nil},
		},
		Renames: map[string]string{queryNamesFunction: function},
		Package: packageName,
	})
}

// ConvertStrings renders convert_strings.go with helpers for binding and reading enum
// values through Go strings. It supports slices, pointer slices, map values and optional
// parameter pointers.
//
// Takes packageName (string) which is the generated package's name.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes conversions (StringConversions) which says which functions the file holds.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func ConvertStrings(packageName string, vocabulary Vocabulary, conversions StringConversions) (sqlmodel.GeneratedFile, error) {
	return renderEmbedded(ConvertStringsFile, vocabulary, &Data{
		Flags: map[string]bool{
			"slice": conversions.Slices, "pointers": conversions.Pointers, "map": conversions.Maps,
			"dereference": conversions.Dereferences,
		},
		Values:  nil,
		Renames: nil,
		Package: packageName,
	})
}

// Sortable renders sortable.go: the OrderDirection type and its OrderAsc and OrderDesc
// values, which every sortable query of a package shares.
//
// Takes packageName (string) which is the generated package's name.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func Sortable(packageName string, vocabulary Vocabulary) (sqlmodel.GeneratedFile, error) {
	return renderEmbedded(sortableFile, vocabulary, &Data{Flags: nil, Values: nil, Renames: nil, Package: packageName})
}

// renderEmbedded renders one shared helper's source into a generated file of the same
// name.
//
// Takes name (string) which is the source's file name, which the generated file keeps.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes data (*Data) which holds the flags, values, renames and package name.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be read or rendered.
func renderEmbedded(name string, vocabulary Vocabulary, data *Data) (sqlmodel.GeneratedFile, error) {
	return preparedSources.File(name, name, vocabulary, data)
}

// regexpList writes a []*regexp.Regexp literal compiling each pattern, for a source that
// imports regexp itself.
//
// Takes patterns ([]string) which are the patterns, in order.
//
// Returns Code which is the literal.
func regexpList(patterns []string) Code {
	var builder strings.Builder
	builder.WriteString("[]*regexp.Regexp{")
	for index, pattern := range patterns {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("regexp.MustCompile(" + goLiteral(pattern) + ")")
	}
	builder.WriteString("}")
	return Code{Text: builder.String(), Imports: nil}
}

// goLiteral writes a string as a Go literal: raw when it can be, so a regular expression
// reads as written, and quoted otherwise.
//
// Takes text (string) which is the string.
//
// Returns string which is the literal.
func goLiteral(text string) string {
	if goast.RawStringAllowed(text) {
		return "`" + text + "`"
	}
	return strconv.Quote(text)
}

// allowList writes a map[string]bool literal holding every entry, one per line.
//
// Takes entries ([]string) which are the allowed values, in order.
//
// Returns Code which is the literal.
func allowList(entries []string) Code {
	var builder strings.Builder
	builder.WriteString("map[string]bool{\n")
	for _, entry := range entries {
		builder.WriteString(strconv.Quote(entry) + ": true,\n")
	}
	builder.WriteString("}")
	return Code{Text: builder.String(), Imports: nil}
}
