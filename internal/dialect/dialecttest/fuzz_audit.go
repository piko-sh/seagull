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
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// auditMultipleFiles is the file count above which a case holds more than one migration
// and one query file.
const auditMultipleFiles = 2

// fuzzAudit records how deeply golden seeds exercise the pipeline.
type fuzzAudit struct {
	// admitted counts bounded seeds.
	admitted int

	// excluded counts seeds beyond the envelope limits.
	excluded int

	// rejected counts expected fatal or diagnostic failures.
	rejected int

	// analysed counts analysed queries.
	analysed int

	// rendered counts successful template checks.
	rendered int

	// generated counts cases producing Go files.
	generated int

	// configured counts custom function configurations.
	configured int

	// multiple counts cases with several files.
	multiple int

	// migrationOnly counts seeds without query files.
	migrationOnly int
}

// AuditFuzzCorpus measures the pipeline stages reached by golden seeds.
//
// Takes t (*testing.T) which reports exclusions and failed expectations.
// Takes newDialect (func() dialect.DialectPort) which supplies a fresh engine.
// Takes directory (string) which holds the golden cases.
func AuditFuzzCorpus(t *testing.T, newDialect func() dialect.DialectPort, directory string) {
	t.Helper()
	var counts fuzzAudit
	bundles := fuzzBundles(t, directory)
	for _, bundle := range bundles {
		data, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fuzzcase.DecodeBundle(data); err != nil {
			counts.excluded++
			t.Logf("excluded %s: %v", bundle.Origin, err)
			continue
		}
		counts.admitted++
		t.Run(filepath.Base(bundle.Origin), func(t *testing.T) { auditFuzzBundle(t, newDialect(), bundle, &counts) })
	}
	if counts.admitted == 0 || counts.analysed == 0 || counts.rendered == 0 || counts.generated == 0 {
		t.Fatal("corpus audit reached no meaningful oracle")
	}
	t.Logf("corpus stages: %+v", counts)
	auditStructuredFeatures(t, newDialect)
	auditScenarioFeatures(t, newDialect)
}

// auditStructuredFeatures requires every enabled feature to reach its assertions.
//
// Takes t (*testing.T) which reports coverage gaps.
// Takes newDialect (func() dialect.DialectPort) which supplies fresh engines.
func auditStructuredFeatures(t *testing.T, newDialect func() dialect.DialectPort) {
	t.Helper()
	engine := newDialect().Info().Name
	covered := make(map[string]int)
	seeds := fuzzcase.Seeds(engine)
	for index, seed := range seeds {
		model, ok := fuzzcase.Decode(engine, seed)
		if !ok {
			t.Fatalf("positive seed %d was rejected", index)
		}
		if t.Run(fmt.Sprintf("structured_%03d", index), func(t *testing.T) {
			checkStructuredQuery(t, newDialect, model)
		}) {
			for _, feature := range model.Features() {
				covered[feature]++
			}
		}
	}
	for _, feature := range fuzzcase.RequiredFeatures(engine) {
		if covered[feature] == 0 {
			t.Errorf("enabled feature %s reached no successful oracle", feature)
		}
	}
	auditStructuredRejections(t, newDialect)
	t.Logf("structured coverage for %s from %d seeds: %v", engine, len(seeds), covered)
}

// auditFuzzBundle validates a golden seed and records successful stages.
//
// Takes t (*testing.T) which reports failures.
// Takes engine (dialect.DialectPort) which owns the analysis.
// Takes bundle (fuzzcase.Bundle) which retains the source files.
// Takes counts (*fuzzAudit) which accumulates coverage.
func auditFuzzBundle(t *testing.T, engine dialect.DialectPort, bundle fuzzcase.Bundle, counts *fuzzAudit) {
	t.Helper()
	queries := auditFuzzFiles(bundle, counts)
	spec := readAuditSpec(t, bundle.Origin)
	ctx, cancel := fuzzContext(t)
	defer cancel()
	generator, err := app.New(engine, app.WithEmitter(&recordingEmitter{}), app.WithCustomFunctions(spec.CustomFunctions...))
	if err != nil {
		t.Fatal(err)
	}
	input := fuzzInput(bundle.FS())
	details, err := generator.GenerateDetailed(ctx, input)
	if spec.ShouldError {
		counts.rejected++
		if err == nil {
			t.Fatal("negative corpus case no longer fails fatally")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range checkExpectedDiagnostics(spec, details.Report.Diagnostics) {
		t.Error(problem)
	}
	generator, err = app.New(engine, app.WithCustomFunctions(spec.CustomFunctions...))
	if err != nil {
		t.Fatal(err)
	}
	details, err = generator.GenerateDetailed(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	auditFuzzResults(t, engine, bundle, details, counts)
	auditGeneratedOutput(t, details, queries, counts)
}

// readAuditSpec reads the expectations of one golden case.
//
// Takes t (*testing.T) which reports a missing or malformed specification.
// Takes directory (string) which is the case directory.
//
// Returns goldenSpec which holds the expectations.
func readAuditSpec(t *testing.T, directory string) goldenSpec {
	t.Helper()
	var spec goldenSpec
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	content, err := root.ReadFile("testspec.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &spec); err != nil {
		t.Fatalf("%s: %v", directory, err)
	}
	return spec
}

// auditGeneratedOutput requires a positive case to reach analysis and emitted files.
//
// Takes t (*testing.T) which reports missing output.
// Takes details (*app.DetailedResult) which holds the pipeline results.
// Takes queries (int) which is the number of query files.
// Takes counts (*fuzzAudit) which accumulates coverage.
func auditGeneratedOutput(t *testing.T, details *app.DetailedResult, queries int, counts *fuzzAudit) {
	t.Helper()
	if details.Result.Err() != nil {
		counts.rejected++
		return
	}
	if queries > 0 && len(details.Report.Queries) == 0 {
		t.Fatal("positive query files reached no analysis")
	}
	if len(details.Result.Files) > 0 {
		counts.generated++
	} else if queries > 0 {
		t.Fatal("positive case emitted no files")
	}
}

// auditFuzzFiles counts seed configuration and source layouts.
//
// Takes bundle (fuzzcase.Bundle) which holds the seed files.
// Takes counts (*fuzzAudit) which accumulates coverage.
//
// Returns int which is the number of query files.
func auditFuzzFiles(bundle fuzzcase.Bundle, counts *fuzzAudit) int {
	if len(bundle.Functions) > 0 && string(bundle.Functions) != "null" {
		counts.configured++
	}
	if len(bundle.Files) > auditMultipleFiles {
		counts.multiple++
	}
	queries := 0
	for _, file := range bundle.Files {
		if path.Dir(file.Name) == queryDirectory {
			queries++
		}
	}
	if queries == 0 {
		counts.migrationOnly++
	}
	return queries
}

// auditFuzzResults checks successful query templates and counts analysis stages.
//
// Takes t (*testing.T) which reports failures.
// Takes engine (dialect.DialectPort) which supplies lexical facts.
// Takes bundle (fuzzcase.Bundle) which identifies the seed.
// Takes details (*app.DetailedResult) which holds pipeline results.
// Takes counts (*fuzzAudit) which accumulates coverage.
func auditFuzzResults(t *testing.T, engine dialect.DialectPort, bundle fuzzcase.Bundle, details *app.DetailedResult, counts *fuzzAudit) {
	t.Helper()
	counts.analysed += len(details.Report.Queries)
	info := engine.Info()
	failed := failedFuzzQueries(details.Report)
	for _, query := range details.Report.Queries {
		if failed[query] {
			continue
		}
		if len(query.Template.Segments) == 0 {
			t.Errorf("positive query %s reached no template", query.Name)
			continue
		}
		for _, problem := range templateProblems(&info, bundle.Origin, query) {
			t.Error(problem)
		}
		counts.rendered++
	}
}

// auditStructuredRejections requires every negative seed to reach its expected error.
//
// Takes t (*testing.T) which reports incorrect diagnostic behaviour.
// Takes newDialect (func() dialect.DialectPort) which supplies fresh engines.
func auditStructuredRejections(t *testing.T, newDialect func() dialect.DialectPort) {
	t.Helper()
	engine := newDialect().Info().Name
	seeds := fuzzcase.RejectionSeeds()
	for index, seed := range seeds {
		rejected, ok := fuzzcase.DecodeRejection(engine, seed)
		if !ok {
			t.Fatalf("rejection seed %d was not decoded", index)
		}
		t.Run(fmt.Sprintf("rejection_%03d", index), func(t *testing.T) { checkStructuredRejection(t, newDialect, rejected) })
	}
	t.Logf("structured rejection coverage for %s from %d seeds", engine, len(seeds))
}
