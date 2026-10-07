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

package thirdparty_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var publicPackages = []string{
	"seagull.sh/seagull/sdk/dialect",
	"seagull.sh/seagull/sdk/emit",
	"seagull.sh/seagull/sdk/emit/emit_kit",
}

func TestTheEmitterImportsOnlyPublicPackages(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		content, err := os.ReadFile(file)
		require.NoError(t, err)
		parsed, err := parser.ParseFile(token.NewFileSet(), file, content, parser.ImportsOnly)
		require.NoError(t, err)
		for _, specification := range parsed.Imports {
			importPath, err := strconv.Unquote(specification.Path.Value)
			require.NoError(t, err)
			standard := !strings.Contains(strings.SplitN(importPath, "/", 2)[0], ".")
			require.True(t, standard || slices.Contains(publicPackages, importPath), "%s imports %s", file, importPath)
		}
		checked++
	}
	require.NotZero(t, checked)
}
