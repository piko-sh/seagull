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

package lexdiff_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/lexdiff"
)

func TestCorpus(t *testing.T) {
	t.Parallel()

	files, err := lexdiff.Corpus("sqlite", "no_such_engine")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	corpora := make(map[string]bool, len(files))
	for _, file := range files {
		require.True(t, strings.HasSuffix(file.Path, "/"+file.Name), file.Path)
		require.True(t, strings.HasSuffix(file.Name, ".sql"), file.Name)
		require.NotContains(t, file.Path, `\`)
		corpora[file.Corpus] = true
	}
	require.Equal(t, map[string]bool{
		lexdiff.CorpusDialect:       true,
		lexdiff.CorpusCompatibility: true,
		lexdiff.CorpusIntegration:   true,
	}, corpora)
}
