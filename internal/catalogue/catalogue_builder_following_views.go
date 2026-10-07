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

package catalogue

import (
	"context"
	"maps"
	"reflect"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxViewDerivationRounds bounds how many rounds of deriving again the views that follow
	// their tables one change starts, so views whose columns keep changing, as views reading
	// each other in a cycle can, cannot hold up the replay.
	maxViewDerivationRounds = 64
)

var (
	// relationChanges lists the mutation kinds that change the columns a table or view has,
	// or which relation a name stands for, after which the views that follow their tables
	// are derived again.
	relationChanges = []ast.MutationKind{
		ast.MutationCreateTable,
		ast.MutationDropTable,
		ast.MutationAlterTableAddColumn,
		ast.MutationAlterTableDropColumn,
		ast.MutationAlterTableAlterColumn,
		ast.MutationAlterTableRenameColumn,
		ast.MutationAlterTableRenameTable,
		ast.MutationAlterTableSetSchema,
		ast.MutationCreateView,
		ast.MutationDropView,
	}

	// vanishedReferenceCodes lists the problems that say a view's definition no longer reads
	// cleanly, as when a column it names was renamed or dropped.
	vanishedReferenceCodes = []string{diag.CodeUnknownColumn, diag.CodeUnknownTable}
)

// followingView is a view of an engine that reads a view's definition anew whenever a
// query reads it, with what its columns are derived from.
type followingView struct {
	// view is the catalogue's view, whose columns are replaced in place.
	view *sqlmodel.View

	// definition is the analysed query the view is defined by.
	definition *ast.RawQueryAnalysis

	// reads holds the keys of the names of the tables and views the definition reads, so the
	// view is derived again only after a change to one of them.
	reads map[string]struct{}

	// declared holds the column names the CREATE VIEW declares, if any.
	declared []sqlmodel.Column

	// readsAny is true when the definition nests its queries too deeply for the relations it
	// reads to be listed, so a change to any relation derives the view again.
	readsAny bool
}

// readsAnyOf reports whether the view's definition reads a relation of one of the given
// names.
//
// Takes names (map[string]struct{}) which holds the keys of the names.
//
// Returns bool which is true when the definition reads one of them.
func (following *followingView) readsAnyOf(names map[string]struct{}) bool {
	if following.readsAny {
		return len(names) > 0
	}
	for name := range names {
		if _, reads := following.reads[name]; reads {
			return true
		}
	}
	return false
}

// followView keeps a view created from a definition, where the engine reads a view's
// definition anew whenever a query reads it, so its columns follow later changes to its
// tables.
//
// Takes view (*sqlmodel.View) which is the view just created.
// Takes mutation (*ast.CatalogueMutation) which holds its definition and declared names.
func (b *catalogueBuilder) followView(view *sqlmodel.View, mutation *ast.CatalogueMutation) {
	if mutation.ViewDefinition == nil || !b.engine.Info().ViewsFollowTheirTables {
		return
	}
	reads, readsAny := b.relationsRead(mutation.ViewDefinition)
	b.followingViews = append(b.followingViews, followingView{
		view:       view,
		definition: mutation.ViewDefinition,
		reads:      reads,
		declared:   slices.Clone(mutation.Columns),
		readsAny:   readsAny,
	})
}

// changedRelations returns the keys of the names of the relations a mutation may have
// changed: the table or view it names, the name it gives it, and every table that
// inherits from the table.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation just applied.
//
// Returns map[string]struct{} which holds the keys.
func (b *catalogueBuilder) changedRelations(mutation *ast.CatalogueMutation) map[string]struct{} {
	changed := make(map[string]struct{}, 2)
	for _, name := range []string{mutation.TableName, mutation.NewName} {
		if name != "" {
			changed[b.key(name)] = struct{}{}
		}
	}
	if len(b.inheritances) == 0 {
		return changed
	}
	index := newInheritanceIndex(b.inheritances)
	for _, table := range []*sqlmodel.Table{
		b.catalogue.LookupTable(mutation.SchemaName, mutation.TableName),
		b.catalogue.LookupTable(mutation.SchemaName, mutation.NewName),
		b.catalogue.LookupTable(mutation.NewName, mutation.TableName),
	} {
		if table == nil {
			continue
		}
		inheritors, _ := index.descendants(table)
		for _, inheritor := range inheritors {
			changed[b.key(inheritor.Name)] = struct{}{}
		}
	}
	return changed
}

// deriveFollowingViews derives again the columns of the views that follow their tables
// and read a changed relation, and then of the views that read a view whose columns
// changed, until none changes.
//
// Each round goes through the views in the order they were created, so a view reading an
// earlier view takes its new columns in the same round. A view the catalogue no longer
// holds is forgotten.
//
// When the views have not settled after maxViewDerivationRounds rounds, or one more round
// than there are views, a Q071 warning is reported on the statement.
//
// Takes changed (map[string]struct{}) which holds the keys of the names of the relations
// the statement changed.
func (b *catalogueBuilder) deriveFollowingViews(ctx context.Context, changed map[string]struct{}) {
	if !slices.ContainsFunc(b.followingViews, func(following followingView) bool { return following.readsAnyOf(changed) }) {
		return
	}
	b.applyPendingColumnOverrides()
	dirty := changed
	for range min(len(b.followingViews)+1, maxViewDerivationRounds) {
		dirty = b.deriveRound(ctx, dirty)
		if len(dirty) == 0 || ctx.Err() != nil {
			return
		}
	}
	b.statementDiagnostics = append(b.statementDiagnostics, diagbuild.Block(b.statement.filename, max(b.statement.line, 1),
		diag.CodeViewDerivationUnsettled, diag.SeverityWarning,
		"the columns of the views that follow their tables did not settle after this statement, so some views may "+
			"have columns the engine does not report; look for views that read each other in a cycle"))
}

// deriveRound derives again, in creation order, every following view that reads a
// relation named in dirty or a view whose columns changed earlier in the round, and
// forgets every view the catalogue no longer holds.
//
// Takes dirty (map[string]struct{}) which holds the keys of the names of the relations
// that changed.
//
// Returns map[string]struct{} which holds the keys of the names of the views whose
// columns changed.
func (b *catalogueBuilder) deriveRound(ctx context.Context, dirty map[string]struct{}) map[string]struct{} {
	changed := make(map[string]struct{})
	kept := b.followingViews[:0]
	for index := range b.followingViews {
		following := b.followingViews[index]
		if !b.holdsView(following.view) {
			continue
		}
		kept = append(kept, following)
		if ctx.Err() != nil || (!following.readsAnyOf(dirty) && !following.readsAnyOf(changed)) {
			continue
		}
		if b.deriveViewAgain(ctx, &following) {
			changed[b.key(following.view.Name)] = struct{}{}
		}
	}
	clear(b.followingViews[len(kept):])
	b.followingViews = kept
	return changed
}

// holdsView reports whether the catalogue still holds a view under its name.
//
// Takes view (*sqlmodel.View) which is the view.
//
// Returns bool which is true when the view's schema files it under its name.
func (b *catalogueBuilder) holdsView(view *sqlmodel.View) bool {
	schema := b.catalogue.Schemas[b.key(view.Schema)]
	return schema != nil && schema.Views[b.key(view.Name)] == view
}

// relationsRead lists the tables and views a view's definition reads, in every query it
// nests: its FROM and JOIN items, its CTEs, its compound branches, its derived tables and
// the subqueries its expressions and predicates hold.
//
// Takes definition (*ast.RawQueryAnalysis) which is the definition.
//
// Returns map[string]struct{} which holds the keys of the names it reads.
// Returns bool which is true when the queries nest more deeply than
// resolve.MaxExpressionResolveDepth, so the list may be incomplete.
func (b *catalogueBuilder) relationsRead(definition *ast.RawQueryAnalysis) (map[string]struct{}, bool) {
	reads := make(map[string]struct{})
	type nestedQuery struct {
		query *ast.RawQueryAnalysis
		depth int
	}
	pending := []nestedQuery{{query: definition, depth: 0}}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current.query == nil {
			continue
		}
		if current.depth >= resolve.MaxExpressionResolveDepth {
			return reads, true
		}
		for _, query := range b.readRelations(current.query, reads) {
			pending = append(pending, nestedQuery{query: query, depth: current.depth + 1})
		}
	}
	return reads, false
}

// readRelations records the tables and views one query reads directly and returns the
// queries it nests.
//
// Takes query (*ast.RawQueryAnalysis) which is the query.
// Takes reads (map[string]struct{}) which receives the keys of the names it reads.
//
// Returns []*ast.RawQueryAnalysis which holds the queries it nests.
func (b *catalogueBuilder) readRelations(query *ast.RawQueryAnalysis, reads map[string]struct{}) []*ast.RawQueryAnalysis {
	read := func(tables []ast.TableReference, joins []ast.JoinClause) {
		for index := range tables {
			reads[b.key(tables[index].Name)] = struct{}{}
		}
		for index := range joins {
			reads[b.key(joins[index].Table.Name)] = struct{}{}
		}
	}
	read(query.FromTables, query.JoinClauses)
	nested := slices.Clone(query.PredicateSubqueries)
	nested = append(nested, query.InsertSelect)
	for index := range query.CompoundBranches {
		nested = append(nested, query.CompoundBranches[index].Query)
	}
	for index := range query.RawDerivedTables {
		nested = append(nested, query.RawDerivedTables[index].InnerQuery)
	}
	for index := range query.CTEDefinitions {
		definition := &query.CTEDefinitions[index]
		read(definition.FromTables, definition.JoinClauses)
		nested = append(nested, definition.Query)
		for branch := range definition.CompoundBranches {
			nested = append(nested, definition.CompoundBranches[branch].Query)
		}
		nested = appendExpressionSubqueries(nested, definition.OutputColumns, nil)
	}
	return appendExpressionSubqueries(nested, query.OutputColumns, query.ClauseExpressions)
}

// deriveViewAgain refreshes a view's columns from its definition, preserving comments and
// overrides on columns with matching names. If the definition no longer resolves, the
// columns remain unchanged.
//
// Takes following (*followingView) which is the view and its definition.
//
// Returns bool which is true when the view's columns changed.
func (b *catalogueBuilder) deriveViewAgain(ctx context.Context, following *followingView) bool {
	columns, problems := b.resolveViewColumns(ctx, following.definition)
	if slices.ContainsFunc(problems, leavesColumnsUnresolved) {
		return false
	}
	nameDeclaredColumns(columns, following.declared)
	columns = padDeclaredColumns(columns, following.declared)
	for index := range columns {
		columns[index].Origin = following.view.Origin
		b.keepViewColumnSettings(&columns[index], following.view.Columns)
	}
	if reflect.DeepEqual(columns, following.view.Columns) {
		return false
	}
	following.view.Columns = columns
	return true
}

// keepViewColumnSettings gives a column derived again the comment and the overrides the
// view's column of the same name had.
//
// Takes column (*sqlmodel.Column) which is the column derived again, changed in place.
// Takes previous ([]sqlmodel.Column) which are the view's columns before.
func (b *catalogueBuilder) keepViewColumnSettings(column *sqlmodel.Column, previous []sqlmodel.Column) {
	index := slices.IndexFunc(previous, func(candidate sqlmodel.Column) bool { return b.sameName(candidate.Name, column.Name) })
	if index < 0 {
		return
	}
	earlier := &previous[index]
	if earlier.Comment != "" {
		column.Comment = earlier.Comment
	}
	if earlier.SQLTypeOverride != "" {
		column.SQLTypeOverride = earlier.SQLTypeOverride
	}
	if earlier.GoTypeOverride != nil {
		column.GoTypeOverride = cloneGoType(earlier.GoTypeOverride)
	}
	if earlier.NullableOverride != nil {
		column.NullableOverride = new(*earlier.NullableOverride)
	}
	if earlier.NonNullElements != nil {
		column.NonNullElements = new(*earlier.NonNullElements)
	}
	if earlier.StructTags != nil {
		column.StructTags = maps.Clone(earlier.StructTags)
	}
}

// changesRelationColumns reports whether a mutation kind may change the columns of a
// table or view a view reads.
//
// Takes kind (ast.MutationKind) which is the mutation's kind.
//
// Returns bool which is true when the views that follow their tables must be derived
// again.
func changesRelationColumns(kind ast.MutationKind) bool {
	return slices.Contains(relationChanges, kind)
}

// appendExpressionSubqueries appends the subqueries held by EXISTS and scalar subquery
// expressions in output columns and clause expressions, at any depth of the expressions.
//
// Takes nested ([]*ast.RawQueryAnalysis) which receives the subqueries.
// Takes columns ([]ast.RawOutputColumn) which are output columns.
// Takes clauses ([]ast.ClauseExpression) which are clause expressions.
//
// Returns []*ast.RawQueryAnalysis which is nested with the subqueries appended.
func appendExpressionSubqueries(
	nested []*ast.RawQueryAnalysis,
	columns []ast.RawOutputColumn,
	clauses []ast.ClauseExpression,
) []*ast.RawQueryAnalysis {
	collect := func(expression ast.Expression) {
		for node := range ast.Preorder(expression) {
			if inner, holds := ast.InnerQuery(node); holds {
				nested = append(nested, inner)
			}
		}
	}
	for index := range columns {
		collect(columns[index].Expression)
	}
	for index := range clauses {
		collect(clauses[index].Expression)
	}
	return nested
}

// leavesColumnsUnresolved reports whether a problem found resolving a view's definition
// leaves its columns unknown: it names a column or relation that is no longer there, or
// the dialect's analysis of it holds a nil node.
//
// Takes problem (*resolve.Error) which is the problem.
//
// Returns bool which is true for an unknown column or relation, or a nil node.
func leavesColumnsUnresolved(problem *resolve.Error) bool {
	return problem != nil && (slices.Contains(vanishedReferenceCodes, problem.Code) || problem.Code == diag.CodeInternalNilGuard)
}
