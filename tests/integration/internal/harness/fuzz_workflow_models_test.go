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

package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkflowModelsRotateEveryModel(t *testing.T) {
	t.Parallel()
	want := []string{workflowRuntime, workflowGrammar, workflowTyped, workflowBuilder, workflowDML, workflowLifecycle}
	for _, engine := range []string{enginePostgres, engineTimescaleDB, engineCockroachDB, engineMySQL, engineMariaDB, engineSQLite, engineDuckDB, engineClickHouse} {
		models, excluded := workflowModels(NativeFuzzSuite{FuzzSuite: FuzzSuite{Engine: engine}})
		names := make([]string, 0, len(models))
		for _, model := range models {
			names = append(names, model.name)
			require.NotEmpty(t, model.seeds, "%s %s has no seeds", engine, model.name)
		}
		require.Equal(t, want, names, "%s rotates other models", engine)
		require.Empty(t, excluded, "%s excludes models", engine)
	}
}
