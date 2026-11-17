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

//go:build integration

package compile_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

func TestParameterlessBatchPackagesCompile(t *testing.T) {
	requirements := targetRequirements(t)
	checksums, err := os.ReadFile("go.sum")
	require.NoError(t, err)
	for _, target := range targets() {
		for _, emitter := range target.emitters {
			t.Run(target.name+"/"+emitter.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeoutCause(t.Context(), compileInputDeadline, errCompileDeadline)
				defer cancel()
				model, ok := fuzzcase.Decode(target.name, []byte{fuzzcase.Version})
				require.True(t, ok, "the base model does not decode")
				files := generateTargetPackage(ctx, t, target, emitter, strings.Join(model.Migrations(), "\n"),
					"-- sgl.query(InsertRows, batch)\nINSERT INTO fuzz_items (id, category) VALUES (1, 2);")
				output, err := buildGeneratedPackage(ctx, t, target, files, requirements, checksums)
				require.NoError(t, err, "%s", output)
			})
		}
	}
}
