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

package pipeline

import (
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// analysis is what analyse finds: the catalogue, the analysed queries and every
// diagnostic, and which of the errors are some query's own.
type analysis struct {
	// catalogue holds the schema.
	catalogue *sqlmodel.Catalogue

	// collector gathers the run's diagnostics: the analysis's, and then the emission's.
	collector *diagnosticCollector

	// refused holds the queries with an error diagnostic of their own, in order.
	refused []*sqlmodel.AnalysedQuery

	// queries holds every analysed query, those refused included, in order.
	queries []*sqlmodel.AnalysedQuery

	// diagnostics holds every diagnostic of the analysis, as the collector gave them once
	// the analysis was done.
	diagnostics []diag.SourceError

	// catalogueDiagnostics holds only diagnostics from building the catalogue.
	catalogueDiagnostics []diag.SourceError

	// failedBlocks counts the query blocks with an error diagnostic of their own that gave
	// no query, such as one whose header is malformed.
	failedBlocks int

	// blocks counts the query blocks read, those that gave no query included.
	blocks int

	// packageErrors reports an error diagnostic that is no one query's own: from a migration
	// or the catalogue, two queries of one name, or an analysis limit.
	packageErrors bool
}

// refuse records that a query has an error diagnostic of its own.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
func (a *analysis) refuse(query *sqlmodel.AnalysedQuery) {
	a.refused = append(a.refused, query)
}

// acceptedQueries returns the queries without an error diagnostic of their own, in order.
//
// Returns []*sqlmodel.AnalysedQuery which holds them.
func (a *analysis) acceptedQueries() []*sqlmodel.AnalysedQuery {
	if len(a.refused) == 0 {
		return a.queries
	}
	return withoutQueries(a.queries, a.refused)
}

// emission is what emitAccepted finds.
type emission struct {
	// methods maps the name of each query the files generate to the name of the method that
	// runs it, as the naming plan gave it.
	methods map[string]string

	// files holds the generated files, or none when an error stopped emission.
	files []sqlmodel.GeneratedFile

	// leftOut counts the queries the emitter's CheckerPort refused and emission left out.
	leftOut int

	// stopped reports an error that stopped emission.
	stopped bool
}

// attempt is what one attempt at emitting a package's queries gives.
type attempt struct {
	// files holds the generated files, or none when a name cannot be planned or the
	// emitter's CheckerPort refuses the request.
	files []sqlmodel.GeneratedFile

	// methods maps the name of each query the plan names to the name of the method that runs
	// it.
	methods map[string]string

	// diagnostics holds a diagnostic for every name that cannot be planned and for every
	// problem the emitter's CheckerPort finds.
	diagnostics []diag.SourceError

	// refused holds the queries the plan or the CheckerPort refused with an error, when
	// every error is one query's own, and is nil otherwise.
	refused []*sqlmodel.AnalysedQuery
}

// plannedMethods maps the name of each query of a request to the name of the method its
// naming plan gives it.
//
// Takes request (*emit.Request) which holds the queries and their plan.
//
// Returns map[string]string which holds the methods, or nil when the plan names none.
func plannedMethods(request *emit.Request) map[string]string {
	planned := request.Naming.Queries
	if len(planned) == 0 {
		return nil
	}
	methods := make(map[string]string, len(planned))
	for index, query := range request.Queries {
		if index < len(planned) && planned[index].Method != "" {
			methods[query.Name] = planned[index].Method
		}
	}
	return methods
}

// refusedQueries returns the queries an emitter's CheckerPort refuses with an error, when
// every problem of error severity names its query, and nil when one is about the request
// as a whole.
//
// Takes problems ([]emit.Problem) which are the CheckerPort's problems.
//
// Returns []*sqlmodel.AnalysedQuery which holds the refused queries, each once.
func refusedQueries(problems []emit.Problem) []*sqlmodel.AnalysedQuery {
	var refused []*sqlmodel.AnalysedQuery
	seen := make(map[*sqlmodel.AnalysedQuery]struct{})
	for index := range problems {
		if problems[index].Severity != diag.SeverityError {
			continue
		}
		if problems[index].Query == nil {
			return nil
		}
		if _, repeated := seen[problems[index].Query]; repeated {
			continue
		}
		seen[problems[index].Query] = struct{}{}
		refused = append(refused, problems[index].Query)
	}
	return refused
}

// withoutQueries returns the queries, in order, without those left out. A query left out
// that the queries do not hold, such as a copy of one of them, changes nothing.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries; never changed.
// Takes leftOut ([]*sqlmodel.AnalysedQuery) which are the queries to leave out.
//
// Returns []*sqlmodel.AnalysedQuery which holds the queries kept, in a new slice.
func withoutQueries(queries, leftOut []*sqlmodel.AnalysedQuery) []*sqlmodel.AnalysedQuery {
	excluded := make(map[*sqlmodel.AnalysedQuery]struct{}, len(leftOut))
	for _, query := range leftOut {
		excluded[query] = struct{}{}
	}
	kept := make([]*sqlmodel.AnalysedQuery, 0, len(queries))
	for _, query := range queries {
		if _, left := excluded[query]; !left {
			kept = append(kept, query)
		}
	}
	return kept
}
