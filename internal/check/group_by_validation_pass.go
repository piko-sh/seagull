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
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
)

// groupByValidationPass validates the query header's group_by keys.
//
// It raises Q014 when a group_by column is not found in the output, Q015 when group_by
// requires a sgl.embed on non-key tables, and Q016 when group_by is used with a command
// other than many.
type groupByValidationPass struct{}

// Analyse validates group_by directive constraints.
//
// Takes subject (*DiagnosticContext) which holds the query and analysis state.
//
// Returns []diag.SourceError which holds any Q014, Q015, or Q016 diagnostics.
func (*groupByValidationPass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	if len(subject.Query.GroupByKey) == 0 {
		return nil
	}

	var diagnostics []diag.SourceError

	if subject.Query.Command != sqlmodel.QueryCommandMany {
		diagnostics = append(diagnostics, diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeGroupByWrongCommand,
			diag.SeverityWarning, fmt.Sprintf(
				"%s is valid only with the many command; query %q uses %s",
				subject.Prefixes.Qualify(roleGroupBy), subject.Query.Name, subject.Query.Command,
			)))
	}

	outputColumnNames := lowercasedOutputColumnNameSet(subject.Query.OutputColumns)

	diagnostics = append(diagnostics, validateGroupByColumnReferences(subject, outputColumnNames)...)
	diagnostics = append(diagnostics, validateGroupByRequiresEmbed(subject)...)

	return diagnostics
}

// validateGroupByColumnReferences checks that each group_by column exists in the query
// output.
//
// Takes subject (*DiagnosticContext) which holds the query state.
// Takes outputColumnNames (map[string]struct{}) which holds the lowercase output column
// names.
//
// Returns []diag.SourceError which holds any Q014 diagnostics for missing columns.
func validateGroupByColumnReferences(subject *DiagnosticContext, outputColumnNames map[string]struct{}) []diag.SourceError {
	var diagnostics []diag.SourceError
	for _, groupByColumn := range subject.Query.GroupByKey {
		columnName := groupByColumn
		if _, unqualified, found := strings.CutLast(groupByColumn, "."); found {
			columnName = unqualified
		}
		if _, exists := outputColumnNames[strings.ToLower(columnName)]; !exists {
			diagnostics = append(diagnostics, diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeGroupByColumnMissing,
				diag.SeverityWarning, fmt.Sprintf(
					"%s references column %q which is not in the query output",
					subject.Prefixes.Qualify(roleGroupBy), groupByColumn,
				)))
		}
	}
	return diagnostics
}

// validateGroupByRequiresEmbed checks that at least one sgl.embed directive exists when
// group_by is used. When the analyser already reported a table the query leaves
// unembedded, the query is not reported a second time.
//
// Takes subject (*DiagnosticContext) which holds the query state.
//
// Returns []diag.SourceError which holds a Q015 diagnostic if no embed directive is
// found.
func validateGroupByRequiresEmbed(subject *DiagnosticContext) []diag.SourceError {
	for index := range subject.ExistingDiagnostics {
		if subject.ExistingDiagnostics[index].Code == diag.CodeGroupByMissingEmbed {
			return nil
		}
	}
	hasEmbed := false
	for index := range subject.Query.OutputColumns {
		if subject.Query.OutputColumns[index].IsEmbedded {
			hasEmbed = true
			break
		}
	}

	if !hasEmbed {
		return []diag.SourceError{diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeGroupByMissingEmbed,
			diag.SeverityWarning, fmt.Sprintf(
				"%s on query %q requires at least one %s directive on non-key tables",
				subject.Prefixes.Qualify(roleGroupBy), subject.Query.Name, subject.Prefixes.Qualify(directive.RoleEmbed),
			))}
	}

	return nil
}
