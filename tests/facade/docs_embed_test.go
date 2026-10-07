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

package facade_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/docs"
)

const docsDirectory = "../../docs"

func TestDocsEmbedEveryPublishedFile(t *testing.T) {
	err := filepath.WalkDir(docsDirectory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".md", ".svg", ".png", ".jpg", ".json":
		default:
			return nil
		}
		relative, err := filepath.Rel(docsDirectory, path)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := docs.FS.ReadFile(filepath.ToSlash(relative))
		if !assert.NoError(t, err, "published file %s is missing from docs.FS", relative) {
			return nil
		}
		assert.True(t, bytes.Equal(got, want), "embedded content differs: %s", relative)
		return nil
	})
	require.NoError(t, err)
}
