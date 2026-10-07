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
	"context"
	"seagull.sh/seagull/internal/catalogue"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
)

// DetailedGeneration adds the first checker report and schema to a generation.
type DetailedGeneration struct {
	// Catalogue is the schema snapshot taken before emission.
	Catalogue *sqlmodel.Catalogue

	// Queries holds independent copies of the first analysed queries.
	Queries []*sqlmodel.AnalysedQuery

	// Diagnostics holds analysis and first checker diagnostics.
	Diagnostics []diag.SourceError

	// CatalogueDiagnostics holds diagnostics from schema construction alone.
	CatalogueDiagnostics []diag.SourceError

	// Generation is the generated output and its final diagnostics.
	Generation Generation
}

// capture preserves precisely the queries and diagnostics a standalone Vet returns: the
// analysis's diagnostics, followed by the first checker diagnostics once they are
// collected.
//
// Takes analysed (analysis) which holds the first analysis result to snapshot and the
// collector of the run's diagnostics.
func (details *DetailedGeneration) capture(analysed analysis) {
	details.Queries = catalogue.CloneQueries(analysed.queries)
	details.Diagnostics = analysed.collector.collected()
}

// GenerateDetailed shares catalogue construction and query analysis with Generate. Report
// data is copied before an emitter or a later checker pass can change it.
//
// Takes request (Request) which holds the input sources and generation settings.
//
// Returns *DetailedGeneration which owns the generation result, first checker report and
// catalogue snapshot.
// Returns error when generation cannot complete.
func (p *Pipeline) GenerateDetailed(ctx context.Context, request Request) (*DetailedGeneration, error) {
	details := new(DetailedGeneration)
	generation, err := p.generate(ctx, request, details)
	if err != nil {
		return nil, err
	}
	details.Generation = generation
	return details, nil
}
