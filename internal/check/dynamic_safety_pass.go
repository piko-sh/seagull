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
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/sqlmodel"
)

// dynamicSafetyPass validates that sgl.sortable parameters only reference columns that
// appear in the query output.
//
// It reports names absent from the projection and emits diagnostics for the dynamic:
// runtime path. Q022 fires when the query's template marks a WHERE condition written in
// the base SQL, so the user understands the merged predicates will be ANDed against the
// static filter rather than producing a duplicate WHERE. Q023 and Q024 report a count
// template that wraps the query and a query with none. A statement terminator needs no
// diagnostic: the builder writes its additions at the places the query's template marks,
// before it.
type dynamicSafetyPass struct{}

// Analyse validates sortable parameter column references and reports the runtime-builder
// diagnostics described on the type.
//
// Takes subject (*DiagnosticContext) which holds the query and analysis state.
//
// Returns []diag.SourceError which holds sortable-column and runtime-builder diagnostics.
func (*dynamicSafetyPass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	var diagnostics []diag.SourceError

	outputColumnNames := lowercasedOutputColumnNameSet(subject.Query.OutputColumns)

	for _, directive := range subject.ParameterDirectives {
		if directive.Kind != sqlmodel.ParameterDirectiveSortable {
			continue
		}

		for _, columnName := range directive.Columns {
			if _, exists := outputColumnNames[strings.ToLower(columnName)]; !exists {
				diagnostics = append(diagnostics, diagbuild.Block(
					subject.Filename,
					subject.Query.Line,
					diag.CodeSortableColumnMissing,
					diag.SeverityWarning,
					fmt.Sprintf(
						"sortable parameter %q references column %q which is not in the query output",
						directive.Name, columnName,
					),
				))
			}
		}
	}

	if subject.Query.DynamicRuntime {
		diagnostics = append(diagnostics, baseQueryShapeDiagnostics(subject)...)
	}

	return diagnostics
}

// baseQueryShapeDiagnostics emits the runtime-builder-only diagnostics that observe the
// shape of the base SQL: an existing WHERE clause (Q022), a count that wraps the query
// (Q023) and a query with no count (Q024).
//
// It is kept separate so the main Analyse path stays focused on sortable column safety.
//
// Takes subject (*DiagnosticContext) which holds the query and analysis state.
//
// Returns []diag.SourceError which holds the base-SQL shape diagnostics.
func baseQueryShapeDiagnostics(subject *DiagnosticContext) []diag.SourceError {
	var diagnostics []diag.SourceError

	if hasCondition(&subject.Query.Template) {
		diagnostics = append(diagnostics, diagbuild.Block(
			subject.Filename,
			subject.Query.Line,
			diag.CodeRuntimeBuilderBaseHasWhere,
			diag.SeverityHint,
			"a dynamic: runtime query already has a WHERE clause; "+
				"runtime predicates are appended with AND, not as a new WHERE",
		))
	}

	if subject.CountWrapped {
		diagnostics = append(diagnostics, diagbuild.Block(
			subject.Filename,
			subject.Query.Line,
			diag.CodeCountSemanticsWrapped,
			diag.SeverityHint,
			"a dynamic: runtime query returns rows other than those it reads, "+
				"through GROUP BY, HAVING, DISTINCT, an aggregate or window function, or a compound; "+
				".Count(ctx) wraps the original in a subquery so the count reflects outer-result rows "+
				"rather than the underlying table",
		))
	}

	if subject.Query.DynamicRuntime && subject.Query.CountTemplate == nil {
		diagnostics = append(diagnostics, diagbuild.Block(
			subject.Filename,
			subject.Query.Line,
			diag.CodeCountRewriteUnavailable,
			diag.SeverityWarning,
			"a dynamic: runtime query could not be rewritten into a COUNT query; "+
				".Count(ctx) is unavailable for this query",
		))
	}

	return diagnostics
}

// hasCondition reports whether a query's template marks a WHERE condition written in the
// query, which the runtime builder's predicates join with AND.
//
// Takes template (*sqlmodel.SQLTemplate) which is the query's template.
//
// Returns bool which is true when its filter segment is present.
func hasCondition(template *sqlmodel.SQLTemplate) bool {
	for index := range template.Segments {
		segment := &template.Segments[index]
		if segment.Kind == sqlmodel.SegmentFilter {
			return segment.Present
		}
	}
	return false
}

// lowercasedOutputColumnNameSet builds a set of lowercase output column names for
// case-insensitive lookups. Several diagnostic passes need this lookup shape, so
// centralising the helper keeps the per-pass code focused on its validation rule rather
// than the lookup ceremony.
//
// Takes columns ([]sqlmodel.OutputColumn) which holds the query's output columns.
//
// Returns map[string]struct{} which is the lowercase column-name set.
func lowercasedOutputColumnNameSet(columns []sqlmodel.OutputColumn) map[string]struct{} {
	result := make(map[string]struct{}, len(columns))
	for index := range columns {
		result[strings.ToLower(columns[index].Name)] = struct{}{}
	}
	return result
}
