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
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestBindLimitsExportsTheErrorItReturnsPastTheCap(t *testing.T) {
	t.Parallel()

	file, err := BindLimits("generated", acme, 999)
	require.NoError(t, err)

	source := string(file.Content)
	require.Equal(t, "bind_limits.go", file.Name)
	require.Contains(t, source, "const acmeMaxBindVariables = 999")
	require.Contains(t, source, `var ErrAcmeTooManyBindVariables = errors.New("acme: too many bind variables")`)
	require.Contains(t, source, "// ErrAcmeTooManyBindVariables is returned")
	require.NotContains(t, source, "errAcme")
}

func TestConvertStringsHoldsTheConversionsCalledNamedAfterTheVocabulary(t *testing.T) {
	t.Parallel()

	slices := "func acmeConvertStrings[To, From ~string](values []From) []To {"
	pointers := "func acmeConvertStringPointers[To, From ~string](values []*From) []*To {"
	maps := "func acmeConvertStringMap[To ~string, Key comparable, From ~string](values map[Key]From) map[Key]To {"
	dereference := "func acmeDereference[T any](value *T) T {"
	testCases := []struct {
		name        string
		conversions StringConversions
		wants       []string
		unwanted    []string
	}{
		{name: "a slice", conversions: StringConversions{Slices: true}, wants: []string{slices}, unwanted: []string{pointers, maps}},
		{name: "a slice of pointers", conversions: StringConversions{Pointers: true}, wants: []string{pointers}, unwanted: []string{slices, maps}},
		{name: "a map", conversions: StringConversions{Maps: true}, wants: []string{maps}, unwanted: []string{slices, pointers}},
		{name: "every conversion", conversions: StringConversions{Slices: true, Pointers: true, Maps: true}, wants: []string{slices, pointers, maps}, unwanted: []string{dereference}},
		{name: "a slice read through a pointer", conversions: StringConversions{Slices: true, Dereferences: true}, wants: []string{slices, dereference}, unwanted: []string{pointers, maps}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := ConvertStrings("generated", acme, testCase.conversions)
			require.NoError(t, err)

			require.Equal(t, "convert_strings.go", file.Name)
			for _, want := range testCase.wants {
				require.Contains(t, string(file.Content), want)
			}
			for _, unwanted := range testCase.unwanted {
				require.NotContains(t, string(file.Content), unwanted)
			}
			require.NotContains(t, string(file.Content), "sgl")
		})
	}
}

func TestErrorWrappingNamesItsHelpersAfterTheVocabularyAndTypeChecks(t *testing.T) {
	t.Parallel()

	file, err := ErrorWrapping("generated", acme)
	require.NoError(t, err)

	source := string(file.Content)
	require.Equal(t, ErrorWrappingFile, file.Name)
	require.Contains(t, source, "func acmeWrapError(label string, err error) error {")
	require.Contains(t, source, "func acmeWrapResult[T any](label string) func(T, error) (T, error) {")
	require.Contains(t, source, `return fmt.Errorf("%s: %w", label, err)`)
	require.NotContains(t, source, "sgl")

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
	require.NoError(t, err)
	_, err = (&types.Config{Importer: importer.Default()}).Check("generated", fileSet, []*ast.File{parsed}, nil)
	require.NoError(t, err)
}

func martianSyntax(form dialect.PlaceholderSigil) RuntimeSyntax {
	return RuntimeSyntax{
		ColumnExpression: `^(?i:x)$`,
		Lists:            dialect.ListSyntax{EmptyMembership: "IN (NONE)", EmptyExclusion: "NOT IN (NONE)"},
		Pagination: dialect.PaginationSyntax{
			Limit:          dialect.Affix{Before: "FETCH NEXT ", After: " ROWS ONLY"},
			Offset:         dialect.Affix{Before: "OFFSET ", After: " ROWS"},
			UnboundedLimit: "",
			OffsetFirst:    true,
		},
		Form:             form,
		Operators:        []string{"=", "LIKE"},
		JSONPathOpenings: []string{"path("},
		RootOperators:    []string{"~>", "~~"},
		CastPatterns:     []string{`^(?i:convert\(\s*(x)\s+to\s+(whole)\s*\))$`},
		Casts:            []dialect.CastForm{{Before: "CONVERT(", Middle: " TO ", After: ")"}},
		CastTypes:        []string{"WHOLE"},
	}
}

func TestRuntimeHelpersWriteTheSyntaxTheyAreGiven(t *testing.T) {
	t.Parallel()

	numbered := martianSyntax(dialect.PlaceholderSigil{Prefix: "@p", Kind: dialect.PlaceholderNumbered})
	anonymous := martianSyntax(dialect.PlaceholderSigil{Prefix: "?", Kind: dialect.PlaceholderAnonymous})
	anonymous.Pagination.OffsetFirst = false
	anonymous.Pagination.UnboundedLimit = "-1"
	anonymous.JSONPathOpenings = nil
	anonymous.RootOperators = nil
	testCases := []struct {
		name    string
		syntax  RuntimeSyntax
		want    []string
		unwants []string
	}{
		{
			name:   "numbered placeholders with path operators, functions and casts",
			syntax: numbered,
			want: []string{
				`builder.WriteString("@p")`,
				"\t\"LIKE\": true,",
				"regexp.MustCompile(`^(?i:x)$`)",
				"regexp.MustCompile(`^(?i:convert\\(\\s*(x)\\s+to\\s+(whole)\\s*\\))$`)",
				`var acmeCastBefore = []string{"CONVERT("}`,
				`const acmeLimitBefore = "FETCH NEXT "`,
				`const acmeEmptyMembership = "IN (NONE)"`,
				`if len(trimmed) > len("path(") && strings.EqualFold(trimmed[:len("path(")], "path(") {`,
				`if arrowIndex := strings.Index(trimmed, "~>"); arrowIndex > 0 {`,
				"\twriteOffset()\n\twriteLimit()\n",
				`var ErrAcmeUnknownColumn = errors.New("acme: unknown column in runtime query filter")`,
			},
			unwants: []string{"acmeBindNumbered", "acmeOffsetFirst"},
		},
		{
			name:    "anonymous placeholders without path syntax",
			syntax:  anonymous,
			want:    []string{`builder.WriteString("?")`, "\twriteLimit()\n\twriteOffset()\n", `const acmeUnboundedLimit = "-1"`},
			unwants: []string{"strconv", "arrowIndex", "strings.EqualFold(trimmed"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := RuntimeHelpers("generated", acme, &testCase.syntax)
			require.NoError(t, err)

			source := string(file.Content)
			for _, want := range testCase.want {
				require.Contains(t, source, want)
			}
			for _, unwant := range testCase.unwants {
				require.NotContains(t, source, unwant)
			}
		})
	}
}

func TestSliceHelpersWriteASuffixOnlyForAFormThatHasOne(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		form    dialect.PlaceholderSigil
		want    []string
		unwants []string
	}{
		{
			name:    "a numbered form without a suffix writes the prefix and the number",
			form:    dialect.PlaceholderSigil{Prefix: "$", Suffix: "", Kind: dialect.PlaceholderNumbered},
			want:    []string{`builder.WriteString("$")`, "strconv.AppendInt(digits[:0], int64(first+index), decimalBase)"},
			unwants: []string{`builder.WriteString("")`},
		},
		{
			name:    "an anonymous form without a suffix writes the marker alone",
			form:    dialect.PlaceholderSigil{Prefix: "?", Suffix: "", Kind: dialect.PlaceholderAnonymous},
			want:    []string{`builder.WriteString("?")`},
			unwants: []string{`builder.WriteString("")`, "strconv.Itoa"},
		},
		{
			name: "a form with a suffix writes it after each placeholder",
			form: dialect.PlaceholderSigil{Prefix: "#", Suffix: "#", Kind: dialect.PlaceholderNumbered},
			want: []string{`builder.WriteString("#")
		var digits [20]byte
		builder.Write(strconv.AppendInt(digits[:0], int64(first+index), decimalBase))
		builder.WriteString("#")`},
			unwants: []string{`builder.WriteString("")`},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := SliceHelpers("generated", acme, testCase.form)
			require.NoError(t, err)

			source := string(file.Content)
			for _, want := range testCase.want {
				require.Contains(t, source, want)
			}
			for _, unwant := range testCase.unwants {
				require.NotContains(t, source, unwant)
			}
		})
	}
}

func TestSharedHelpersCompileTogetherInEveryForm(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		marker   rune
		numbered bool
	}{
		{name: "numbered", marker: '#', numbered: true},
		{name: "anonymous", marker: '#', numbered: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := renderAll(t,
				func() (sqlmodel.GeneratedFile, error) { return BindLimits("generated", acme, 7) },
				func() (sqlmodel.GeneratedFile, error) {
					form := dialect.PlaceholderSigil{Prefix: string(testCase.marker), Kind: dialect.PlaceholderAnonymous}
					if testCase.numbered {
						form.Kind = dialect.PlaceholderNumbered
					}
					return SliceHelpers("generated", acme, form)
				},
				func() (sqlmodel.GeneratedFile, error) {
					form := dialect.PlaceholderSigil{Prefix: string(testCase.marker), Kind: dialect.PlaceholderAnonymous}
					if testCase.numbered {
						form.Kind = dialect.PlaceholderNumbered
					}
					return RuntimeHelpers("generated", acme, new(martianSyntax(form)))
				},
				func() (sqlmodel.GeneratedFile, error) { return InlineJSON("generated", acme, true, true) },
				func() (sqlmodel.GeneratedFile, error) { return Sortable("generated", acme) },
				func() (sqlmodel.GeneratedFile, error) {
					return ConvertStrings("generated", acme, StringConversions{Slices: true, Pointers: true, Maps: true})
				},
				func() (sqlmodel.GeneratedFile, error) {
					return QueryNames("generated", acme, "names.go", "NameOf", []QueryName{{Constant: "listThings", Name: "ListThings"}},
						[]QueryNamePrefix{{Constant: "listThings", Name: "ListThings", Length: 0}})
				},
				func() (sqlmodel.GeneratedFile, error) {
					return sqlmodel.GeneratedFile{Name: "things.go", Content: []byte("package generated\n\nconst listThings = \"\"\n")}, nil
				},
			)
			requireTypeChecks(t, files)
		})
	}
}

func TestQueryNamesNameTheLookupAsConfigured(t *testing.T) {
	t.Parallel()

	file, err := QueryNames("generated", acme, "otel.go", "QueryNameResolver", []QueryName{
		{Constant: "listTasks", Name: "ListTasks"},
		{Constant: "createTask", Name: "CreateTask"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, "otel.go", file.Name)
	require.Equal(t, "package generated\n\n"+
		"// queryNameMap associates exact generated SQL with query names.\n"+
		"var queryNameMap = map[string]string{listTasks: \"ListTasks\", createTask: \"CreateTask\"}\n\n"+
		"// QueryNameResolver identifies generated SQL by exact text or a configured prefix.\n//\n"+
		"// Takes query (string) which is the SQL text passed to the driver.\n//\n"+
		"// Returns string which is the generated query name, or empty when no query matches.\n"+
		"func QueryNameResolver(query string) string {\n\treturn queryNameMap[query]\n}\n", string(file.Content))
}

func TestQueryNamesLookUpEditedSQLByItsPrefix(t *testing.T) {
	t.Parallel()

	file, err := QueryNames("generated", acme, "otel.go", "QueryNameResolver",
		[]QueryName{{Constant: "listTasks", Name: "ListTasks"}},
		[]QueryNamePrefix{{Constant: "listTasks", Name: "ListTasks", Length: 31}})
	require.NoError(t, err)

	source := string(file.Content)
	require.Contains(t, source, `var queryNamePrefixes = []string{listTasks[:31]}`)
	require.Contains(t, source, `var queryNamePrefixNames = []string{"ListTasks"}`)
	require.Contains(t, source, "\tfor index, prefix := range queryNamePrefixes {\n")
	require.NotContains(t, source, "queryNamePrefixed")
}

func renderAll(t *testing.T, renderers ...func() (sqlmodel.GeneratedFile, error)) []sqlmodel.GeneratedFile {
	t.Helper()
	files := make([]sqlmodel.GeneratedFile, 0, len(renderers))
	for _, render := range renderers {
		file, err := render()
		require.NoError(t, err)
		files = append(files, file)
	}
	return files
}

func requireTypeChecks(t *testing.T, files []sqlmodel.GeneratedFile) {
	t.Helper()
	fileSet := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(files))
	for _, file := range files {
		syntax, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
		require.NoErrorf(t, err, "%s:\n%s", file.Name, file.Content)
		parsed = append(parsed, syntax)
	}
	config := types.Config{Importer: importer.Default()}
	_, err := config.Check("generated", fileSet, parsed, nil)
	require.NoError(t, err, strings.Join(fileNames(files), ", "))
}

func fileNames(files []sqlmodel.GeneratedFile) []string {
	names := make([]string, len(files))
	for index, file := range files {
		names[index] = file.Name
	}
	return names
}
