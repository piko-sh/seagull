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

package valuereading_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/internal/valuereading"
	"seagull.sh/seagull/internal/gogen"
)

func vocabularyWith(helperPrefix string) gogen.Vocabulary {
	request := &emit.Request{}
	request.Style.HelperPrefix = helperPrefix
	request.Style.ErrorPrefix = helperPrefix
	return gogen.NewVocabulary(request)
}

func declaredNames(file *ast.File) []string {
	var names []string
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			names = append(names, typed.Name.Name)
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				if typeSpec, isType := spec.(*ast.TypeSpec); isType {
					names = append(names, typeSpec.Name.Name)
				}
			}
		}
	}
	return names
}

func TestRenderDeclaresExactlyTheReadersItListsAfterThePrefix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		prefix string
	}{
		{name: "the canonical prefix", prefix: "sgl"},
		{name: "another prefix", prefix: "acme"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			vocabulary := vocabularyWith(testCase.prefix)
			file, err := valuereading.Render(vocabulary, "store")
			require.NoError(t, err)
			require.Equal(t, valuereading.File, file.Name)

			fileSet := token.NewFileSet()
			parsed, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.SkipObjectResolution)
			require.NoError(t, err)
			require.Equal(t, "store", parsed.Name.Name)
			_, err = (&types.Config{Importer: importer.Default()}).Check("store", fileSet, []*ast.File{parsed}, nil)
			require.NoError(t, err, "the rendered readers type-check on their own")

			declared := declaredNames(parsed)
			want := make([]string, 0, len(valuereading.Helpers))
			for _, suffix := range valuereading.Helpers {
				want = append(want, vocabulary.Helper(suffix))
			}
			slices.Sort(declared)
			slices.Sort(want)
			require.Equal(t, want, declared)
		})
	}
}
