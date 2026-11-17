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

package pgx

import (
	"go/build/constraint"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHelperSourcesThatImportPgxAreIgnoredByTheModule(t *testing.T) {
	t.Parallel()

	entries, err := sources.ReadDir("internal/source")
	require.NoError(t, err)
	for _, entry := range entries {
		content, err := sources.ReadFile("internal/source/" + entry.Name())
		require.NoError(t, err)
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), content, parser.ImportsOnly|parser.ParseComments)
		require.NoError(t, err)
		importsPgx := false
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			importsPgx = importsPgx || strings.HasPrefix(path, "github.com/jackc/")
		}
		if !importsPgx {
			continue
		}
		ignored := false
		for _, group := range file.Comments {
			for _, comment := range group.List {
				expression, err := constraint.Parse(comment.Text)
				if err == nil {
					ignored = ignored || expression.String() == "ignore"
				}
			}
		}
		require.True(t, ignored, "%s imports pgx but is not excluded with //go:build ignore", entry.Name())
	}
}
