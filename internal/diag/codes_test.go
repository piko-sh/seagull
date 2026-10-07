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

package diag_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
)

func declaredCodes(t *testing.T) map[string]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "diagnostic_codes.go", nil, 0)
	require.NoError(t, err)
	codes := make(map[string]string)
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for index, name := range value.Names {
				if !strings.HasPrefix(name.Name, "Code") || index >= len(value.Values) {
					continue
				}
				literal, isLiteral := value.Values[index].(*ast.BasicLit)
				require.True(t, isLiteral, "%s must be a string literal", name.Name)
				unquoted, unquoteErr := strconv.Unquote(literal.Value)
				require.NoError(t, unquoteErr)
				codes[unquoted] = name.Name
			}
		}
	}
	require.NotEmpty(t, codes)
	return codes
}

func TestCodesDocumentEveryDeclaredCode(t *testing.T) {
	declared := declaredCodes(t)
	documented := make(map[string]bool)
	for _, entry := range diag.Codes() {
		assert.False(t, documented[entry.Code], "code %s is documented twice", entry.Code)
		documented[entry.Code] = true
		_, known := declared[entry.Code]
		assert.True(t, known, "code %s is documented but not declared", entry.Code)
	}
	for code, name := range declared {
		assert.True(t, documented[code], "%s (%s) has no documentation in codes.go", name, code)
	}
}

func TestRetiredCodesAreNeverDeclared(t *testing.T) {
	declared := declaredCodes(t)
	for _, retired := range []string{"Q019", "Q021", "Q025"} {
		name, reused := declared[retired]
		assert.False(t, reused, "%s is retired and must not be reused, but %s declares it", retired, name)
	}
}

func TestCodesAreSortedAndComplete(t *testing.T) {
	codes := diag.Codes()
	assert.True(t, slices.IsSortedFunc(codes, func(left, right diag.CodeInfo) int {
		return strings.Compare(left.Code, right.Code)
	}))
	for _, entry := range codes {
		assert.Regexp(t, `^Q\d{3}$`, entry.Code)
		assert.NotEmpty(t, entry.Title, entry.Code)
		assert.NotEmpty(t, entry.Explanation, entry.Code)
		assert.False(t, strings.HasSuffix(entry.Title, "."), entry.Code)
		assert.Equal(t, strings.ToUpper(entry.Title[:1]), entry.Title[:1], entry.Code)
		assert.NotContains(t, entry.Explanation, "\u2014", entry.Code)
		assert.NotContains(t, entry.Explanation, "\u2013", entry.Code)
		assert.LessOrEqual(t, entry.Severity, diag.SeverityHint, entry.Code)
		assert.Equal(t, 0, strings.Count(entry.Explanation, "```")%2, entry.Code)
	}
}

func TestCodesReturnsACopy(t *testing.T) {
	first := diag.Codes()
	first[0].Title = "changed"
	assert.NotEqual(t, "changed", diag.Codes()[0].Title)
}

func TestLookupCode(t *testing.T) {
	testCases := []struct {
		input string
		want  string
		found bool
	}{
		{input: "Q012", want: "Q012", found: true},
		{input: "q012", want: "Q012", found: true},
		{input: "Q12", want: "Q012", found: true},
		{input: "12", want: "Q012", found: true},
		{input: " q1 ", want: "Q001", found: true},
		{input: "Q051", want: "Q051", found: true},
		{input: "q52", want: "Q052", found: true},
		{input: "Q019", found: false},
		{input: "Q021", found: false},
		{input: "Q025", found: false},
		{input: "Q999", found: false},
		{input: "", found: false},
		{input: "Q", found: false},
		{input: "QX12", found: false},
		{input: "-1", found: false},
		{input: "99999999999999999999999", found: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {
			entry, found := diag.LookupCode(testCase.input)
			require.Equal(t, testCase.found, found)
			assert.Equal(t, testCase.want, entry.Code)
		})
	}
}

func TestLookupCodeSeverities(t *testing.T) {
	testCases := map[string]diag.ErrorSeverity{
		diag.CodeUnknownColumn:           diag.SeverityWarning,
		diag.CodeDuplicateQueryName:      diag.SeverityError,
		diag.CodeMultipleStatements:      diag.SeverityError,
		diag.CodeAliasPrefix:             diag.SeverityHint,
		diag.CodeForeignPrefix:           diag.SeverityWarning,
		diag.CodeOptionalNonOneCommand:   diag.SeverityError,
		diag.CodeReturningNotSupported:   diag.SeverityError,
		diag.CodeUnreferencedParameter:   diag.SeverityError,
		diag.CodeDiagnosticLimit:         diag.SeverityWarning,
		diag.CodeDuplicateObject:         diag.SeverityError,
		diag.CodeUnknownObject:           diag.SeverityError,
		diag.CodePartlyAnalysed:          diag.SeverityWarning,
		diag.CodeRecoveredPanic:          diag.SeverityError,
		diag.CodeViewDerivationUnsettled: diag.SeverityWarning,
	}
	for code, severity := range testCases {
		entry, found := diag.LookupCode(code)
		require.True(t, found, code)
		assert.Equal(t, severity, entry.Severity, code)
	}
}

func TestPackageSourcesWriteNoLiteralDashes(t *testing.T) {
	t.Parallel()

	sources, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, sources)
	for _, source := range sources {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(source)
			require.NoError(t, err)
			assert.NotContains(t, string(content), "\u2014")
			assert.NotContains(t, string(content), "\u2013")
		})
	}
}
