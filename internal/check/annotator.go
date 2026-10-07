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

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// roleGroupBy names the group_by keyword in diagnostic messages, rendered with the
	// canonical directive prefix.
	roleGroupBy = "group_by"
)

// DiagnosticContext carries all the information a diagnostic pass needs to analyse a
// single query and produce diagnostics.
type DiagnosticContext struct {
	// Query holds the fully analysed query with resolved types and columns.
	Query *sqlmodel.AnalysedQuery

	// RawAnalysis holds the raw engine analysis output before type resolution.
	RawAnalysis *ast.RawQueryAnalysis

	// Scope holds the scope chain for resolving table and column references.
	Scope *resolve.ScopeChain

	// Filename holds the source file name for diagnostic source locations.
	Filename string

	// ParameterDirectives holds the user-specified parameter type directives parsed from
	// query comments.
	ParameterDirectives []*directive.ParameterDirective

	// ExistingDiagnostics holds the diagnostics already produced for this query before the
	// passes run (directive, parse and type-resolution diagnostics).
	ExistingDiagnostics []diag.SourceError

	// Prefixes holds the directive prefixes of the run; passes render directive names in
	// their messages with its canonical prefix.
	Prefixes namespace.Set

	// CountWrapped reports that the query's count template wraps the query in `SELECT
	// COUNT(*) FROM (<query>) sub` because its rows are not the rows it reads, as with GROUP
	// BY, DISTINCT, an aggregate or a window function, so the count is over the query's
	// result rows.
	CountWrapped bool
}

// DiagnosticAnalyser coordinates SQL diagnostic passes over analysed queries. Each pass
// inspects the query from a different angle and produces structured diagnostics with
// source locations.
//
// A DiagnosticAnalyser is not safe for concurrent use: its passes share a type resolver
// that keeps state while it types a query. Use one per goroutine.
type DiagnosticAnalyser struct {
	// passes holds the ordered list of diagnostic passes to run against each query.
	passes []diagnosticPass
}

// NewDiagnosticAnalyser creates a diagnostic analyser with the standard set of diagnostic
// passes for the given dialect and catalogue. What the passes read of the dialect, its
// Info, its core types and the scope builder of nested queries, is made here, once.
//
// Takes engine (dialect.DialectPort) which is the dialect the queries are written for;
// nil leaves out what depends on it, such as the checks of clause references and of
// asynchronous execution.
// Takes catalogue (*sqlmodel.Catalogue) which provides schema information needed by
// passes such as generated column validation.
//
// Returns *DiagnosticAnalyser which is ready to analyse queries.
func NewDiagnosticAnalyser(engine dialect.DialectPort, catalogue *sqlmodel.Catalogue) *DiagnosticAnalyser {
	facts := newDialectFacts(engine)
	return &DiagnosticAnalyser{
		passes: []diagnosticPass{
			&parameterCountPass{dialect: facts},
			&commandOutputPass{dialect: facts},
			&dynamicSafetyPass{},
			&generatedColumnPass{catalogue: catalogue},
			&insertShapePass{catalogue: catalogue, dialect: facts},
			newClauseReferencePass(engine, facts, catalogue),
			&groupByValidationPass{},
			&sliceCommandValidationPass{},
			&asyncExecPass{dialect: facts},
			&returningSupportPass{dialect: facts},
		},
	}
}

// Analyse runs all registered passes against the given query context and returns the
// collected diagnostics. No pass runs once the context is done.
//
// Takes subject (*DiagnosticContext) which carries the query and scope information for
// analysis.
//
// Returns []diag.SourceError which holds all diagnostics collected from the passes.
func (a *DiagnosticAnalyser) Analyse(ctx context.Context, subject *DiagnosticContext) []diag.SourceError {
	diagnostics := make([]diag.SourceError, 0, len(a.passes))
	for _, pass := range a.passes {
		if ctx.Err() != nil {
			break
		}
		diagnostics = append(diagnostics, pass.Analyse(ctx, subject)...)
	}
	return diagnostics
}

// diagnosticPass is a single diagnostic check that inspects a query and produces zero or
// more SourceError diagnostics. Passes are run sequentially by the DiagnosticAnalyser.
type diagnosticPass interface {
	// Analyse inspects the query context and returns zero or more diagnostics.
	//
	// Takes subject (*DiagnosticContext) which carries the query and scope information for
	// the pass.
	//
	// Returns []diag.SourceError which holds the diagnostics the pass produced.
	Analyse(ctx context.Context, subject *DiagnosticContext) []diag.SourceError
}

// dialectFacts is what the passes read of the dialect, read once when the analyser is
// made rather than once per query.
type dialectFacts struct {
	// void is the dialect's type of a value that holds nothing, or has no engine name when
	// the dialect has none.
	void sqlmodel.SQLType

	// info describes the dialect: its name, placeholders, lexical rules, system columns, row
	// identifier aliases and the statements it accepts RETURNING on.
	info dialect.Info
}

// newDialectFacts reads what the passes need of a dialect.
//
// Takes engine (dialect.DialectPort) which is the dialect, or nil.
//
// Returns *dialectFacts which holds the facts, or nil for a nil dialect.
func newDialectFacts(engine dialect.DialectPort) *dialectFacts {
	if engine == nil {
		return nil
	}
	return &dialectFacts{void: engine.CoreTypes().Void, info: engine.Info()}
}
