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

package session_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/wasm/internal/session"
)

func TestEveryStarterGeneratesCleanly(t *testing.T) {
	t.Parallel()

	listed, err := session.Dialects()
	require.NoError(t, err)

	for _, view := range listed {
		t.Run(view.Name, func(t *testing.T) {
			t.Parallel()

			files, err := session.Starter(view.Name)
			require.NoError(t, err)
			require.Len(t, files, 5)
			require.Contains(t, files["sgl.yaml"], "dialect: "+view.Name+"\n")

			for _, driver := range view.Drivers {
				response := session.Run(t.Context(), session.Request{Files: files, Override: session.Override{Prepared: nil, Dialect: "", Driver: driver}, V: 0})
				require.True(t, response.OK, response.Error)
				require.Zero(t, response.Counts.Errors, response.Diagnostics)
				require.NotEmpty(t, response.Files)
			}
		})
	}
}

func TestStarterRejectsUnknownDialect(t *testing.T) {
	t.Parallel()

	_, err := session.Starter("oracle")

	require.ErrorContains(t, err, "unknown dialect")
}
