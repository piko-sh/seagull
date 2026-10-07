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

package emitkit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit"
)

func TestSanitiseStem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stem     string
		expected string
	}{
		{stem: "users", expected: "users"},
		{stem: "_hidden", expected: "q_hidden"},
		{stem: ".dotted", expected: "q.dotted"},
		{stem: "", expected: "q"},
		{stem: "devices_ios", expected: "devices_ios_sql"},
		{stem: "devices_linux_amd64", expected: "devices_linux_amd64_sql"},
		{stem: "devices_linux_test", expected: "devices_linux_test_sql"},
		{stem: "devices_test", expected: "devices_test"},
		{stem: "linux", expected: "linux"},
		{stem: "linux_amd64", expected: "linux_amd64_sql"},
		{stem: "devices_wasm", expected: "devices_wasm_sql"},
		{stem: "devices_windowsx", expected: "devices_windowsx"},
		{stem: "report_js.monthly", expected: "report_js_sql.monthly"},
		{stem: "report.monthly_js", expected: "report.monthly_js"},
		{stem: "_x_linux", expected: "q_x_linux_sql"},
	}

	for _, test := range tests {
		t.Run(test.stem, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.expected, sanitiseStem(test.stem))
		})
	}
}

func TestSanitiseStemKnowsEveryPlatformOfTheToolchain(t *testing.T) {
	t.Parallel()

	output, err := testkit.GoCommand(t, "", nil, "tool", "dist", "list").Output()
	if err != nil {
		t.Skipf("go tool dist list failed: %v", err)
	}

	platforms := strings.Fields(string(output))
	require.NotEmpty(t, platforms)
	for _, platform := range platforms {
		operatingSystem, architecture, found := strings.Cut(platform, "/")
		require.True(t, found, platform)
		require.Equal(t, "file_"+operatingSystem+"_sql", sanitiseStem("file_"+operatingSystem), operatingSystem)
		require.Equal(t, "file_"+architecture+"_sql", sanitiseStem("file_"+architecture), architecture)
	}
}
