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

package config

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const docsDirectory = "../../../docs"

func TestTheDocumentedConfigurationExamplesParse(t *testing.T) {
	t.Parallel()

	patterns := map[string]*regexp.Regexp{
		"yaml": regexp.MustCompile("(?s)```yaml\\n(version:.*?)\\n```"),
		"json": regexp.MustCompile("(?s)```json\\n(\\{.*?)\\n```"),
	}
	for _, page := range []string{"reference/configuration.md", "how-to/live-schema.md", "how-to/shape-generated-code.md"} {
		content, err := os.ReadFile(filepath.Join(docsDirectory, filepath.FromSlash(page)))
		require.NoError(t, err)
		for language, pattern := range patterns {
			matches := pattern.FindAllSubmatch(content, -1)
			if language == "yaml" {
				assert.NotEmpty(t, matches, "%s has no complete configuration example", page)
			}
			for _, match := range matches {
				_, err := Parse(match[1], language == "json")
				assert.NoError(t, err, "%s: invalid Markdown configuration", page)
			}
		}
	}
}
