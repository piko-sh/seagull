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

package emittest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit"
)

func TestKnownTargetsCoverTheToolchainsTargets(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("lists the toolchain's targets through the go command; skipped under -short")
	}

	output, err := testkit.GoCommand(t, "", nil, "tool", "dist", "list").Output()
	require.NoError(t, err)
	for line := range strings.Lines(string(output)) {
		system, architecture, ok := strings.Cut(strings.TrimSpace(line), "/")
		if !ok {
			continue
		}
		require.True(t, knownOperatingSystems[system], "the operating system %s is missing", system)
		require.True(t, knownArchitectures[architecture], "the architecture %s is missing", architecture)
	}
}
