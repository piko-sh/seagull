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

package check

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestSliceCommandValidationPass(t *testing.T) {
	t.Parallel()
	pass := &sliceCommandValidationPass{}

	t.Run("slice with batch produces Q017 error", func(t *testing.T) {
		t.Parallel()
		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name:    "BatchInsertWithSlice",
				Line:    1,
				Command: sqlmodel.QueryCommandBatch,
				Parameters: []sqlmodel.QueryParameter{
					{Number: 1, Name: "ids", IsSlice: true},
				},
			},
		}
		diagnostics := pass.Analyse(t.Context(), subject)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, diag.CodeSliceBatchCopyFrom, diagnostics[0].Code)
		assert.Equal(t, diag.SeverityError, diagnostics[0].Severity)
		assert.Equal(t, `query "BatchInsertWithSlice": a kind: slice parameter cannot be used with the batch command, `+
			`which sends one row of parameters per item and cannot expand a slice`, diagnostics[0].Message)
	})

	t.Run("slice with copyfrom produces Q017 error", func(t *testing.T) {
		t.Parallel()
		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name:    "CopyWithSlice",
				Line:    1,
				Command: sqlmodel.QueryCommandCopyFrom,
				Parameters: []sqlmodel.QueryParameter{
					{Number: 1, Name: "ids", IsSlice: true},
				},
			},
		}
		diagnostics := pass.Analyse(t.Context(), subject)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, diag.CodeSliceBatchCopyFrom, diagnostics[0].Code)
	})

	t.Run("slice with dynamic runtime produces Q018 error", func(t *testing.T) {
		t.Parallel()
		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name:           "DynamicRuntimeWithSlice",
				Line:           1,
				Command:        sqlmodel.QueryCommandMany,
				DynamicRuntime: true,
				Parameters: []sqlmodel.QueryParameter{
					{Number: 1, Name: "statuses", IsSlice: true},
				},
			},
		}
		diagnostics := pass.Analyse(t.Context(), subject)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, diag.CodeSliceDynamicRuntime, diagnostics[0].Code)
		assert.Equal(t, `query "DynamicRuntimeWithSlice": a kind: slice parameter cannot be used with dynamic: runtime, `+
			`whose builder cannot expand slice placeholders`, diagnostics[0].Message)
	})

	t.Run("slice with sortable is allowed", func(t *testing.T) {
		t.Parallel()
		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name:    "SortableWithSlice",
				Line:    1,
				Command: sqlmodel.QueryCommandMany,
				Parameters: []sqlmodel.QueryParameter{
					{Number: 1, Name: "statuses", IsSlice: true},
					{Number: 2, Name: "order_by", Kind: sqlmodel.ParameterDirectiveSortable},
				},
			},
		}
		diagnostics := pass.Analyse(t.Context(), subject)
		assert.Empty(t, diagnostics)
	})

	t.Run("slice with many command produces no error", func(t *testing.T) {
		t.Parallel()
		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name:    "FetchByStatuses",
				Line:    1,
				Command: sqlmodel.QueryCommandMany,
				Parameters: []sqlmodel.QueryParameter{
					{Number: 1, Name: "statuses", IsSlice: true},
				},
			},
		}
		diagnostics := pass.Analyse(t.Context(), subject)
		assert.Empty(t, diagnostics)
	})

	t.Run("no slice parameters produces no error", func(t *testing.T) {
		t.Parallel()
		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name:    "BatchInsert",
				Line:    1,
				Command: sqlmodel.QueryCommandBatch,
				Parameters: []sqlmodel.QueryParameter{
					{Number: 1, Name: "id", Kind: sqlmodel.ParameterDirectiveParam},
				},
			},
		}
		diagnostics := pass.Analyse(t.Context(), subject)
		assert.Empty(t, diagnostics)
	})

	t.Run("slice with optional is allowed", func(t *testing.T) {
		t.Parallel()
		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name:      "FilterWithSlice",
				Line:      1,
				Command:   sqlmodel.QueryCommandMany,
				IsDynamic: true,
				Parameters: []sqlmodel.QueryParameter{
					{Number: 1, Name: "statuses", IsSlice: true},
					{Number: 2, Name: "priority", IsOptional: true},
				},
			},
		}
		diagnostics := pass.Analyse(t.Context(), subject)
		assert.Empty(t, diagnostics)
	})

	t.Run("slice with limit is allowed", func(t *testing.T) {
		t.Parallel()
		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name:      "LimitedSlice",
				Line:      1,
				Command:   sqlmodel.QueryCommandMany,
				IsDynamic: true,
				Parameters: []sqlmodel.QueryParameter{
					{Number: 1, Name: "statuses", IsSlice: true},
					{Number: 2, Name: "page_size", Context: sqlmodel.ParameterContextLimit},
				},
			},
		}
		diagnostics := pass.Analyse(t.Context(), subject)
		assert.Empty(t, diagnostics)
	})
}
