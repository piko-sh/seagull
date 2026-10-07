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

package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

func TestExplain(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	for _, spelling := range []string{"Q012", "q012", "12"} {
		result := project.Run("explain", spelling)
		requireCode(t, result, 0)
		assert.True(t, strings.HasPrefix(result.Stdout, "Q012: "), result.Stdout)
		assert.Contains(t, result.Stdout, "(error)")
	}

	result := project.Run("explain")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "CODE")
	assert.Contains(t, result.Stdout, "Q001")
	assert.Contains(t, result.Stdout, "Q051")

	result = project.Run("explain", "--format", "markdown")
	requireCode(t, result, 0)
	assert.True(t, strings.HasPrefix(result.Stdout, "| Code | Severity | Title |"))
	assert.Contains(t, result.Stdout, "\n## Q050\n")
	assert.Contains(t, result.Stdout, "[Q001](#q001)")

	result = project.Run("explain", "--format", "markdown", "Q001")
	requireCode(t, result, 0)
	assert.True(t, strings.HasPrefix(result.Stdout, "## Q001\n"))

	requireCode(t, project.Run("explain", "Q999"), 2)
	requireCode(t, project.Run("explain", "Q001", "Q002"), 2)
	requireCode(t, project.Run("explain", "--format", "html"), 2)
}
