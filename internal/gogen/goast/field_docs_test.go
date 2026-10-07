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

package goast_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/gogen/goast"
)

const undocumentedModels = "package store\n\n" +
	"// Posts are articles.\n" +
	"type Posts struct {\n" +
	"\tID       int64  `json:\"id\"`\n" +
	"\tTitle    string `json:\"title\"`\n" +
	"\tAuthorID *int64 `json:\"author_id\"`\n" +
	"\tPlace    struct {\n" +
	"\t\tTitle string\n" +
	"\t}\n" +
	"}\n\n" +
	"type Authors struct {\n" +
	"\tTitle string `json:\"title\"`\n" +
	"}\n"

const documentedModels = "package store\n\n" +
	"// Posts are articles.\n" +
	"type Posts struct {\n" +
	"\tID int64 `json:\"id\"`\n" +
	"\t// The headline,\n" +
	"\t// shown in lists.\n" +
	"\tTitle    string `json:\"title\"`\n" +
	"\tAuthorID *int64 `json:\"author_id\"`\n" +
	"\tPlace    struct {\n" +
	"\t\tTitle string\n" +
	"\t}\n" +
	"}\n\n" +
	"type Authors struct {\n" +
	"\tTitle string `json:\"title\"`\n" +
	"}\n"

func TestDocumentFieldsPutsEachCommentAboveItsField(t *testing.T) {
	t.Parallel()

	documented, err := goast.DocumentFields([]byte(undocumentedModels), map[string]map[string][]string{
		"Posts": {"Title": {"// The headline,", "// shown in lists."}},
	})

	require.NoError(t, err)
	require.Equal(t, documentedModels, string(documented))
}

func TestDocumentFieldsLeavesSourceWithoutCommentsAlone(t *testing.T) {
	t.Parallel()

	documented, err := goast.DocumentFields([]byte(undocumentedModels), nil)

	require.NoError(t, err)
	require.Equal(t, undocumentedModels, string(documented))
}
