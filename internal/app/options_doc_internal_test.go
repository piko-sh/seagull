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

package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/pipeline"
	"seagull.sh/seagull/internal/sqlfile"
)

func optionDocs(t *testing.T) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "options.go", nil, parser.ParseComments)
	require.NoError(t, err)
	docs := make(map[string]string)
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Doc == nil {
			continue
		}
		docs[function.Name.Name] = strings.Join(strings.Fields(function.Doc.Text()), " ")
	}
	require.NotEmpty(t, docs, "options.go holds no documented function")
	return docs
}

func thousands(count int) string {
	digits := strconv.Itoa(count)
	var grouped strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	return grouped.String()
}

func byteSize(size int64) string {
	if size%(1<<40) == 0 {
		return fmt.Sprintf("%d TiB", size>>40)
	}
	return fmt.Sprintf("%d MiB", size>>20)
}

func TestOptionDocsGiveTheDefaultsTheGeneratorApplies(t *testing.T) {
	t.Parallel()

	docs := optionDocs(t)
	tests := []struct {
		option string
		want   string
	}{
		{option: "WithMaxFileBytes", want: "The default is " + byteSize(sqlfile.DefaultMaxFileBytes)},
		{option: "WithMaxFileBytes", want: "at most " + byteSize(sqlfile.MaxFileBytesCeiling)},
		{option: "WithMaxTotalBytes", want: "The default is " + byteSize(sqlfile.DefaultMaxTotalBytes) + "."},
		{option: "WithMaxFiles", want: "The default is " + thousands(sqlfile.DefaultMaxFiles) + "."},
		{option: "WithMaxQueries", want: "The default is " + thousands(pipeline.DefaultMaxQueries) + "."},
		{option: "WithMaxDiagnostics", want: "The default is " + thousands(pipeline.DefaultMaxDiagnostics) + "."},
	}
	for _, testCase := range tests {
		t.Run(testCase.option+" "+testCase.want, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, docs[testCase.option], testCase.want)
		})
	}
}
