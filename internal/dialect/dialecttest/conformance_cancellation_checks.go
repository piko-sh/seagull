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

package dialecttest

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"

	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// runningAnalysisItems is how many items the select list of the statement a running
	// analysis is cancelled in holds, which takes far more reads of the statement's tokens
	// than a parser walks before it next looks at its context.
	runningAnalysisItems = 2000

	// checksBeforeCancelling is how many times the context of a running analysis reports
	// itself live before it reports itself cancelled: once, for the check a dialect makes
	// before it starts the walk.
	checksBeforeCancelling = 1
)

// laggingContext is a context that is cancelled only once it has been asked whether it
// is, a set number of times, so a check can cancel an analysis after it has started.
type laggingContext struct {
	context.Context

	// checks counts the calls of Err.
	checks atomic.Int32
}

// Err reports the context live for its first checksBeforeCancelling calls and cancelled
// after them.
//
// Returns error which is nil, then context.Canceled.
func (c *laggingContext) Err() error {
	if c.checks.Add(1) <= checksBeforeCancelling {
		return nil
	}
	return context.Canceled
}

// checkCancellationStopsARunningAnalysis checks that AnalyseQuery stops when its context
// is cancelled while it walks a statement, returning the context's error, rather than
// finishing, or never finishing when its walk loops.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkCancellationStopsARunningAnalysis(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.BaseMigration == "" {
		return notApplicable("the fixture sets no BaseMigration")
	}
	engine := fixture.NewDialect()
	query := "SELECT " + strings.TrimSuffix(strings.Repeat("id, name, ", runningAnalysisItems), ", ") + " FROM items"
	statements, err := engine.ParseStatements(ctx, query)
	if err != nil || len(statements) != 1 {
		return failed("a long select list over items must parse into one statement, got %d and %v", len(statements), err)
	}
	cancelled := &laggingContext{Context: ctx, checks: atomic.Int32{}}
	analysis, analyseErr := engine.AnalyseQuery(cancelled, emptyCatalogue(), statements[0])
	if !errors.Is(analyseErr, context.Canceled) {
		return failed("AnalyseQuery cancelled while it walks a statement must stop with the context's error, got %v and an analysis of %d columns",
			analyseErr, outputCount(analysis))
	}
	return passed
}

// outputCount returns the number of output columns of an analysis, zero for none.
//
// Takes analysis (*ast.RawQueryAnalysis) which may be nil.
//
// Returns int which is the count.
func outputCount(analysis *ast.RawQueryAnalysis) int {
	if analysis == nil {
		return 0
	}
	return len(analysis.OutputColumns)
}
