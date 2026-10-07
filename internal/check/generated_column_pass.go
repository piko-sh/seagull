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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// generatedColumnPass detects INSERT/UPDATE statements that attempt to write to generated
// (computed) columns. SQLite rejects these at runtime, so we report them at compile time.
type generatedColumnPass struct {
	// catalogue holds the schema state for column lookups.
	catalogue *sqlmodel.Catalogue
}

// Analyse detects writes to generated columns.
//
// Takes subject (*DiagnosticContext) which holds the query and analysis state.
//
// Returns []diag.SourceError which holds any diagnostics for generated column writes.
func (p *generatedColumnPass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	var diagnostics []diag.SourceError

	if subject.RawAnalysis == nil {
		return nil
	}

	for _, reference := range subject.RawAnalysis.ParameterReferences {
		if reference.Context != sqlmodel.ParameterContextAssignment {
			continue
		}
		if reference.ColumnReference == nil {
			continue
		}

		column := p.assignedColumn(subject, reference.ColumnReference)
		if column == nil || !column.IsGenerated {
			continue
		}

		var parameterLabel string
		if reference.Name != "" {
			parameterLabel = fmt.Sprintf("%q", reference.Name)
		} else {
			parameterLabel = fmt.Sprintf("%d", reference.Number)
		}
		line := 0
		if subject.Query != nil {
			line = subject.Query.Line
		}
		diagnostics = append(diagnostics, diagbuild.Block(subject.Filename, line, diag.CodeGeneratedColumn, diag.SeverityWarning,
			fmt.Sprintf("parameter %s assigns to generated column %q which cannot be written to",
				parameterLabel, reference.ColumnReference.ColumnName)))
	}

	return diagnostics
}

// assignedColumn finds the catalogue column a parameter is assigned to. The reference's
// qualifier is resolved through the query's scope to the table it names, in its schema; a
// qualifier the scope does not hold is taken as an unqualified table name.
//
// Takes subject (*DiagnosticContext) which holds the query's scope.
// Takes reference (*ast.ColumnReference) which names the assigned column.
//
// Returns *sqlmodel.Column which is the column, or nil when the catalogue has none.
func (p *generatedColumnPass) assignedColumn(subject *DiagnosticContext, reference *ast.ColumnReference) *sqlmodel.Column {
	schema, table := "", reference.TableAlias
	if subject.Scope != nil {
		if scoped, exists := subject.Scope.Table(reference.TableAlias); exists && scoped.Name != "" {
			schema, table = scoped.Schema, scoped.Name
		}
	}
	return p.catalogue.FindColumn(schema, table, reference.ColumnName)
}
