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

package sqlmodel

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func declaredConstantCounts(t *testing.T) map[string]int {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	counts := make(map[string]int)
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, entry.Name(), nil, 0)
		require.NoError(t, parseErr)
		for _, declaration := range file.Decls {
			countConstants(declaration, counts)
		}
	}
	return counts
}

func countConstants(declaration ast.Decl, counts map[string]int) {
	general, isGeneral := declaration.(*ast.GenDecl)
	if !isGeneral || general.Tok != token.CONST {
		return
	}
	currentType := ""
	for _, spec := range general.Specs {
		valueSpec, isValue := spec.(*ast.ValueSpec)
		if !isValue {
			continue
		}
		if identifier, isIdentifier := valueSpec.Type.(*ast.Ident); isIdentifier {
			currentType = identifier.Name
		} else if len(valueSpec.Values) > 0 {
			currentType = ""
		}
		if currentType != "" {
			counts[currentType] += len(valueSpec.Names)
		}
	}
}

func TestEnumNames(t *testing.T) {
	t.Parallel()

	counts := declaredConstantCounts(t)
	testCases := []struct {
		name     string
		nameOf   func(value uint8) string
		typeName string
	}{
		{typeName: "SQLTypeCategory", nameOf: func(value uint8) string { return SQLTypeCategory(value).String() }},
		{typeName: "QueryCommand", nameOf: func(value uint8) string { return QueryCommand(value).String() }},
		{typeName: "ParameterDirectiveKind", nameOf: func(value uint8) string { return ParameterDirectiveKind(value).String() }},
		{typeName: "ParameterContext", nameOf: func(value uint8) string { return ParameterContext(value).String() }},
		{typeName: "GeneratedKind", nameOf: func(value uint8) string { return GeneratedKind(value).String() }},
		{typeName: "FunctionDataAccess", nameOf: func(value uint8) string { return FunctionDataAccess(value).String() }},
		{typeName: "FunctionNullableBehaviour", nameOf: func(value uint8) string { return FunctionNullableBehaviour(value).String() }},
		{typeName: "ConstraintKind", nameOf: func(value uint8) string { return ConstraintKind(value).String() }},
	}

	for _, testCase := range testCases {
		t.Run(testCase.typeName, func(t *testing.T) {
			t.Parallel()

			declared := counts[testCase.typeName]
			require.Positive(t, declared, "no constants of %s were found", testCase.typeName)
			seen := make(map[string]struct{}, declared)
			for value := range declared {
				name := testCase.nameOf(uint8(value))
				require.NotContains(t, name, "(", "%s value %d has no name", testCase.typeName, value)
				require.NotContains(t, seen, name, "%s names two values %q", testCase.typeName, name)
				seen[name] = struct{}{}
			}
			require.Equal(t, testCase.typeName+"(200)", testCase.nameOf(200))
		})
	}
}

func TestEnumNamesSpelling(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "category", actual: TypeCategoryBinary.String(), expected: "binary"},
		{name: "aggregate state", actual: TypeCategoryAggregateState.String(), expected: "aggregate-state"},
		{name: "command", actual: QueryCommandExecResult.String(), expected: "execresult"},
		{name: "parameter context", actual: ParameterContextFunctionArgument.String(), expected: "function-argument"},
		{name: "constraint", actual: ConstraintPrimaryKey.String(), expected: "primary-key"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, testCase.actual)
		})
	}
}
