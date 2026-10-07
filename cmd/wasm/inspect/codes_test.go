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

package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/wasm/internal/session"
)

func TestCodesReproduceEachCodeOrSayWhyNot(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	status := run(t.Context(), []string{"-codes"}, &stdout, &stderr)

	require.Equal(t, exitOK, status, stderr.String())
	var views []codeView
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &views))
	require.Len(t, views, len(session.Codes()))
	listed := map[string]bool{}
	for _, view := range views {
		listed[view.Code] = true
		project, reproduced := reproductions[view.Code]
		reason, skipped := unreproducible[view.Code]
		require.NotEqual(t, reproduced, skipped, "%s needs a reproduction or a reason it has none, not both", view.Code)
		if skipped {
			require.Nil(t, view.Example, view.Code)
			require.Empty(t, view.Dialect, view.Code)
			require.Equal(t, reason, view.Unreproducible, view.Code)
			continue
		}
		require.NotNil(t, view.Example, "the reproduction for %s no longer raises it", view.Code)
		require.Equal(t, view.Code, view.Example.Code)
		require.NotEmpty(t, view.Example.Text, view.Code)
		require.Equal(t, project.dialect, view.Dialect, view.Code)
		require.Empty(t, view.Unreproducible, view.Code)
	}
	for code := range reproductions {
		require.True(t, listed[code], "%s is reproduced but is not a code", code)
	}
	for code := range unreproducible {
		require.True(t, listed[code], "%s is listed as unreproducible but is not a code", code)
	}
}
