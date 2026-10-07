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

package dialecttest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

var targetOptionVocabulary = []string{
	"WithName",
	"WithMaxParseDepth",
	"WithMaxTokensPerStatement",
	"WithExtraTypes",
	"WithExtraFunctions",
	"WithTypeNormaliserHook",
	"WithImplicitCastHook",
	"WithPromoteTypeHook",
}

var flavourBases = map[string]string{
	"cockroachdb": "postgres",
	"timescaledb": "postgres",
	"mariadb":     "mysql",
}

var expectedOptionGaps = map[string][]string{}

var expectedBuilderShapeGaps = map[string]string{}

type dialectPackage struct {
	files []*ast.File
	name  string
}

func (inspected dialectPackage) optionNames() []string {
	var names []string
	for _, file := range inspected.files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !function.Name.IsExported() || !strings.HasPrefix(function.Name.Name, "With") {
				continue
			}
			results := function.Type.Results
			if results == nil || len(results.List) != 1 {
				continue
			}
			if identifier, isIdentifier := results.List[0].Type.(*ast.Ident); isIdentifier && identifier.Name == "Option" {
				names = append(names, function.Name.Name)
			}
		}
	}
	slices.Sort(names)
	return names
}

func (inspected dialectPackage) builderShapeGap() string {
	aliases := map[string]bool{}
	var builder *ast.StructType
	for _, file := range inspected.files {
		for node := range ast.Preorder(file) {
			specification, ok := node.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if selector, isSelector := specification.Type.(*ast.SelectorExpr); isSelector && specification.Assign.IsValid() &&
				selector.Sel.Name == "CatalogueBuilder" {
				aliases[specification.Name.Name] = true
			}
			if structure, isStruct := specification.Type.(*ast.StructType); isStruct && specification.Name.Name == "FunctionCatalogueBuilder" {
				builder = structure
			}
		}
	}
	if builder == nil {
		return "the dialect has no FunctionCatalogueBuilder"
	}
	embedsKitBuilder := false
	for _, field := range builder.Fields.List {
		typeExpression := field.Type
		if star, isStar := typeExpression.(*ast.StarExpr); isStar {
			typeExpression = star.X
		}
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				if name.IsExported() {
					return "the builder exports its embedded catalogue builder or other fields"
				}
			}
			continue
		}
		switch embedded := typeExpression.(type) {
		case *ast.Ident:
			embedsKitBuilder = embedsKitBuilder || aliases[embedded.Name] && !unicode.IsUpper(rune(embedded.Name[0]))
		case *ast.SelectorExpr:
			return "the builder exports its embedded catalogue builder or other fields"
		}
	}
	if !embedsKitBuilder {
		return "the builder does not embed the kit's catalogue builder"
	}
	return ""
}

func loadDialectPackage(t *testing.T, name string) dialectPackage {
	t.Helper()

	directory := filepath.Join("..", name)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)

	fileSet := token.NewFileSet()
	loaded := dialectPackage{name: name}
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".go") || strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, filepath.Join(directory, fileName), nil, parser.SkipObjectResolution)
		require.NoError(t, parseErr)
		loaded.files = append(loaded.files, file)
	}
	require.NotEmpty(t, loaded.files, "the %s dialect has no Go source under %s", name, directory)
	return loaded
}

func TestBuiltInDialectsShareTheOptionVocabulary(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"postgres", "cockroachdb", "timescaledb", "mysql", "mariadb", "sqlite", "duckdb", "clickhouse"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := name
			if base, isFlavour := flavourBases[name]; isFlavour {
				require.Empty(t, loadDialectPackage(t, name).optionNames(), "a flavour takes its base dialect's options")
				source = base
			}
			inspected := loadDialectPackage(t, source)

			var gaps []string
			for _, option := range targetOptionVocabulary {
				if !slices.Contains(inspected.optionNames(), option) {
					gaps = append(gaps, option)
				}
			}
			slices.Sort(gaps)
			require.Equal(t, expectedOptionGaps[name], gaps,
				"update expectedOptionGaps when a dialect gains or loses part of the shared option vocabulary")

			require.Equal(t, expectedBuilderShapeGaps[name], inspected.builderShapeGap(),
				"update expectedBuilderShapeGaps when a dialect's FunctionCatalogueBuilder adopts the shared shape: "+
					"embed dialectkit.CatalogueBuilder through an unexported alias and export no fields")
		})
	}
}
