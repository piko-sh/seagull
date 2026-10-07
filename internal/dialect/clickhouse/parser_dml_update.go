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

package clickhouse

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// kwReturning is the RETURNING keyword. ClickHouse has no RETURNING clause, so the
	// analysers record it for the analyser to report rather than drop it.
	kwReturning = "RETURNING"

	// kwSet is the SET keyword introducing an UPDATE's assignments.
	kwSet = "SET"
)

var (
	// stopAfterSetClause ends the assignments of a lightweight UPDATE.
	stopAfterSetClause = []string{kwWhere, kwReturning, kwSettings}

	// stopAfterMutationWhere ends the predicate of a lightweight UPDATE or DELETE.
	stopAfterMutationWhere = []string{kwReturning, kwSettings}
)

// analyseUpdate analyses a lightweight `UPDATE [db.]table [ON CLUSTER c] SET col = expr,
// ... [IN PARTITION p] WHERE expr` statement.
//
// It marks the analysis as data-modifying, records the target table, and registers every
// `{name:Type}` placeholder in the assignments as an assignment parameter and every one
// in the predicate as a comparison parameter. A RETURNING clause, which ClickHouse does
// not have, is recorded so the analyser reports it.
//
// Returns *ast.RawQueryAnalysis which holds the table and the parameters.
// Returns error when the UPDATE header is malformed or the parse depth limit is exceeded.
func (p *parser) analyseUpdate() (*ast.RawQueryAnalysis, error) {
	if p.analysisDepth >= p.maxParseDepth {
		return nil, p.depthError()
	}
	p.analysisDepth++
	defer func() { p.analysisDepth-- }()

	analysis := &ast.RawQueryAnalysis{}
	if _, err := p.ExpectKeyword("UPDATE"); err != nil {
		return nil, err
	}
	database, name, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	analysis.FromTables = []ast.TableReference{{Schema: database, Name: name, Alias: ""}}
	_ = p.matchOnCluster()
	if _, err := p.ExpectKeyword(kwSet); err != nil {
		return nil, err
	}
	assignmentsStart := p.Position
	assignmentsEnd := topLevelKeywordIndex(p.Tokens, assignmentsStart, stopAfterSetClause...)
	if assignmentsEnd < 0 {
		assignmentsEnd = len(p.Tokens)
	}
	p.recordAssignments(analysis, assignmentsStart, assignmentsEnd, name)
	p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextAssignment, stopAfterSetClause...)
	p.parseMutationWhere(analysis)
	p.parseMutationTail(analysis)

	if p.analysisDepth == 1 && p.firstParameterTypeError != nil {
		return analysis, p.firstParameterTypeError
	}
	return analysis, nil
}

// parseMutationWhere parses the WHERE clause of a lightweight UPDATE or DELETE when
// present, recording its condition and where it is written.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the condition and its
// placeholders.
func (p *parser) parseMutationWhere(analysis *ast.RawQueryAnalysis) {
	if !p.IsKeyword(kwWhere) {
		return
	}
	start := p.Position
	p.Advance()
	analysis.HasWhereClause = true
	conditionStart := p.Position
	p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, stopAfterMutationWhere...)
	p.parseCondition(analysis, ast.ClauseWhere, conditionStart)
	p.recordSpan(dialectkit.SpanWhere, start, p.Position)
	p.recordSpan(dialectkit.SpanWhereCondition, conditionStart, p.Position)
}

// parseMutationTail reads what may follow a data-changing statement's body: a RETURNING
// clause, which is recorded, and a SETTINGS clause.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the RETURNING clause and any
// placeholders.
func (p *parser) parseMutationTail(analysis *ast.RawQueryAnalysis) {
	p.parseReturningIfPresent(analysis)
	if p.MatchKeyword(kwSettings) {
		p.parseSettingsClause(analysis)
	}
}

// parseReturningIfPresent records an optional RETURNING clause.
//
// ClickHouse has no RETURNING, but the clause is recorded rather than dropped as unparsed
// tokens: the analyser judges it against Info.Returning and reports it, pointing at the
// keyword. Its expression list is consumed so the rest of the statement still parses.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the returning flag, the keyword's
// location and any placeholders in the list.
func (p *parser) parseReturningIfPresent(analysis *ast.RawQueryAnalysis) {
	keyword := p.Current()
	start := p.Position
	if !p.MatchKeyword(kwReturning) {
		return
	}
	analysis.HasReturning = true
	analysis.ReturningLocation = keyword.Start
	p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, kwSettings, kwFormat)
	p.recordSpan(dialectkit.SpanReturning, start, p.Position)
}
