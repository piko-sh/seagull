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
	"context"
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/sqlmodel"
)

// sliceCommandValidationPass rejects kind: slice parameters in queries that cannot expand
// them: a batch or copyfrom query and a dynamic: runtime query.
type sliceCommandValidationPass struct{}

// Analyse checks where the query's kind: slice parameters are used.
//
// Takes subject (*DiagnosticContext) which holds the query and analysis state.
//
// Returns []diag.SourceError which holds any Q017 or Q018 diagnostics.
func (*sliceCommandValidationPass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	hasSlice := false
	for index := range subject.Query.Parameters {
		if subject.Query.Parameters[index].IsSlice {
			hasSlice = true
			break
		}
	}
	if !hasSlice {
		return nil
	}

	var diagnostics []diag.SourceError

	if subject.Query.Command == sqlmodel.QueryCommandBatch ||
		subject.Query.Command == sqlmodel.QueryCommandCopyFrom {
		diagnostics = append(diagnostics, diagbuild.Block(subject.Filename, subject.Query.Line,
			diag.CodeSliceBatchCopyFrom, diag.SeverityError, fmt.Sprintf(
				"query %q: a kind: slice parameter cannot be used with the %s command, which sends one row of parameters per item and cannot expand a slice",
				subject.Query.Name, subject.Query.Command,
			)))
	}

	if subject.Query.DynamicRuntime {
		diagnostics = append(diagnostics, diagbuild.Block(subject.Filename, subject.Query.Line,
			diag.CodeSliceDynamicRuntime, diag.SeverityError, fmt.Sprintf(
				"query %q: a kind: slice parameter cannot be used with dynamic: runtime, whose builder cannot expand slice placeholders",
				subject.Query.Name,
			)))
	}

	return diagnostics
}
