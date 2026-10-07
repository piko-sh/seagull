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

package seagull

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModuleVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		info      *debug.BuildInfo
		name      string
		want      string
		available bool
	}{
		{name: "no build info", info: nil, available: false, want: "devel"},
		{
			name: "dependency",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/host", Version: "v1.0.0"},
				Deps: []*debug.Module{{Path: "seagull.sh/seagull", Version: "v0.1.0-alpha.2"}},
			},
			available: true,
			want:      "0.1.0-alpha.2",
		},
		{
			name:      "main module",
			info:      &debug.BuildInfo{Main: debug.Module{Path: "seagull.sh/seagull", Version: "v0.2.0"}},
			available: true,
			want:      "0.2.0",
		},
		{
			name: "workspace build",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "seagull.sh/seagull/cmd/sgl", Version: "(devel)"},
				Deps: []*debug.Module{{Path: "seagull.sh/seagull", Version: "(devel)"}},
			},
			available: true,
			want:      "devel",
		},
		{
			name: "replaced by a directory",
			info: &debug.BuildInfo{Deps: []*debug.Module{{
				Path:    "seagull.sh/seagull",
				Version: "v0.1.0",
				Replace: &debug.Module{Path: "../seagull"},
			}}},
			available: true,
			want:      "devel",
		},
		{
			name: "replaced by another version",
			info: &debug.BuildInfo{Deps: []*debug.Module{{
				Path:    "seagull.sh/seagull",
				Version: "v0.1.0",
				Replace: &debug.Module{Path: "example.com/fork", Version: "v0.1.1"},
			}}},
			available: true,
			want:      "0.1.1",
		},
		{
			name:      "not linked",
			info:      &debug.BuildInfo{Main: debug.Module{Path: "example.com/host", Version: "v1.0.0"}},
			available: true,
			want:      "devel",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, moduleVersion(testCase.info, testCase.available))
		})
	}
}
