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
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

func TestDialectsListsCapabilities(t *testing.T) {
	t.Parallel()

	result := clitest.NewProject(t).Run("dialects")
	requireCode(t, result, 0)
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	require.Len(t, lines, 9)
	assert.Equal(t, []string{"NAME", "FAMILY", "PARAMETERS", "RETURNING", "MIGRATE", "INTROSPECT", "DRIVER"}, strings.Fields(lines[0]))
	assert.Regexp(t, `^duckdb\s+duckdb\s+\$1\s+INSERT, UPDATE, DELETE\s+yes\s+no\s+none$`, lines[3])
	assert.Regexp(t, `^mariadb\s+mysql\s+\?\s+INSERT, DELETE\s+yes\s+no\s+mysql$`, lines[4])
	assert.Regexp(t, `^mysql\s+mysql\s+\?\s+no\s+yes\s+no\s+mysql$`, lines[5])
	assert.Regexp(t, `^sqlite\s+sqlite\s+\?1\s+INSERT, UPDATE, DELETE\s+yes\s+yes\s+sqlite$`, lines[7])

	requireCode(t, clitest.NewProject(t).Run("dialects", "extra"), 2)
}
