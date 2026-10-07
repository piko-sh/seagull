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

package buildinfo

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/cmd/internal/config"
)

func TestVersionsAreWellFormed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		pattern string
	}{
		{name: "the CLI", version: Version, pattern: `^\S+$`},
		{name: "the library", version: LibraryVersion(), pattern: `^(devel|\d+\.\d+\.\d+\S*)$`},
		{name: "the generated code", version: CodegenVersion(), pattern: `^\d+$`},
		{name: "the Go toolchain", version: GoVersion(), pattern: `^(go\d+\.\d+\S*|unknown)$`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Regexp(t, regexp.MustCompile(testCase.pattern), testCase.version)
		})
	}
}

func TestConfigSchemaVersionIsTheVersionTheReaderEnforces(t *testing.T) {
	t.Parallel()

	assert.Equal(t, config.CurrentVersion, ConfigSchemaVersion())
}
