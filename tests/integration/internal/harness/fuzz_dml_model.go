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

package harness

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzznative"
)

// dmlModelValues is how many values a model row holds, its quantity, note and data.
const dmlModelValues = 3

// dmlQuantityText spells the qty column as text where CAST AS TEXT does not.
var dmlQuantityText = map[string]string{engineMySQL: "CAST(qty AS CHAR)", engineMariaDB: "CAST(qty AS CHAR)", engineClickHouse: "toString(qty)"}

// dmlReferenceRow holds one row read without generated code.
type dmlReferenceRow struct {
	// note holds the note text, which is meaningless when noteNull is set.
	note any

	// data holds the binary value as the driver returns it.
	data any

	// noteNull is the engine's own note IS NULL.
	noteNull any

	// dataNull is the engine's own data IS NULL.
	dataNull any

	// quantity holds the qty column as text.
	quantity string

	// id is the identifier.
	id int64
}

// compare checks the row against the model's row.
//
// Takes want (fuzzcase.DMLRow) which is the model's row.
//
// Returns string which describes the first difference, or nothing.
func (reference dmlReferenceRow) compare(want fuzzcase.DMLRow) string {
	switch {
	case len(want.Values) < dmlModelValues:
		return fmt.Sprintf("the model's row holds %d values, not %d", len(want.Values), dmlModelValues)
	case reference.id != want.ID:
		return fmt.Sprintf("identifier %d, the model expects %d", reference.id, want.ID)
	case reference.quantity != want.Values[0].Number:
		return fmt.Sprintf("quantity %s, the model expects %s", reference.quantity, want.Values[0].Number)
	case truth(reference.dataNull):
		return "data IS NULL in a NOT NULL column"
	case !bytes.Equal(asBytes(reference.data), want.Values[2].Bytes):
		return fmt.Sprintf("data %x, the model expects %x", asBytes(reference.data), want.Values[2].Bytes)
	case truth(reference.noteNull) != want.Values[1].Null:
		return fmt.Sprintf("note IS NULL is %t, the model expects %t", truth(reference.noteNull), want.Values[1].Null)
	case !want.Values[1].Null && string(asBytes(reference.note)) != want.Values[1].Text:
		return fmt.Sprintf("note %q, the model expects %q", asBytes(reference.note), want.Values[1].Text)
	}
	return ""
}

// FuzzDMLModel runs version 17 statement sequences through every execution mode's
// generated exec, returning, batch and copy methods and checks each against an
// independent model of the table and of the engine's affected-row counts.
//
// Takes f (*testing.F) which owns the worker and seeds.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
func FuzzDMLModel(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	for _, seed := range fuzzcase.DMLSeeds(suite.Engine) {
		f.Add(seed)
	}
	worker := newModeWorker(f)
	var packages []modePackage
	var once, metadata sync.Once
	f.Fuzz(func(t *testing.T, data []byte) {
		model, ok := fuzzcase.DecodeDML(suite.Engine, data)
		if !ok {
			t.Skip("unsupported version 17 envelope")
		}
		once.Do(func() { packages = dmlPackages(t, suite) })
		checkDMLModel(t, suite, worker, packages, &model, &metadata)
	})
}

// checkDMLModel runs one decoded version 17 sequence in every mode and checks it.
//
// Takes t (*testing.T) which reports differences from the model.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
// Takes worker (*modeWorker) which owns the helper.
// Takes packages ([]modePackage) which holds each mode's generated catalogue.
// Takes model (*fuzzcase.DMLCase) which supplies the statements and outcomes.
// Takes metadata (*sync.Once) which records the server and capabilities once.
func checkDMLModel(t *testing.T, suite NativeFuzzSuite, worker *modeWorker, packages []modePackage, model *fuzzcase.DMLCase, metadata *sync.Once) {
	t.Helper()
	require.NotEmpty(t, packages, "the version 17 catalogue did not generate")
	process := worker.start(t, suite, packages)
	problems := make([]string, 0, len(packages))
	for _, item := range packages {
		problems = append(problems, runDMLMode(t, suite, process, item.mode, model, metadata)...)
	}
	require.Empty(t, problems, "version 17 input %v:\n%s\nstatements:\n%s", model.Data, strings.Join(problems, "\n"), describeDML(model))
}

// runDMLMode runs a sequence in one mode on a fresh table and checks it.
//
// Takes t (*testing.T) which reports setup failures.
// Takes suite (NativeFuzzSuite) which supplies connections and the driver.
// Takes process (*fuzznative.Process) which serves every mode.
// Takes mode (string) which names the execution mode.
// Takes model (*fuzzcase.DMLCase) which supplies the statements and outcomes.
// Takes metadata (*sync.Once) which records the server and capabilities once.
//
// Returns []string which lists the mode's differences from the model.
func runDMLMode(t *testing.T, suite NativeFuzzSuite, process *fuzznative.Process, mode string, model *fuzzcase.DMLCase, metadata *sync.Once) []string {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(modeInputDeadline), errFuzzInputDeadline)
	defer cancel()
	database, connection := suite.OpenNative(t)
	database.SetMaxOpenConns(1)
	metadata.Do(func() {
		writeFuzzEngineMetadata(ctx, t, database, suite.Engine)
		writeFuzzCapabilities(t, "dml", suite.Engine, fuzzcase.DMLCapabilities(suite.Engine))
	})
	fuzzApply(ctx, t, database, fuzzcase.DMLSchema(suite.Engine))
	require.NoError(t, database.Close())
	response, err := process.ExchangeModes(ctx, fuzznative.ModeRequest{Connection: connection, Mode: mode, Calls: nil, DML: model.Data, ID: 0, Version: 0})
	require.NoError(t, err)
	require.True(t, response.Error == "" && response.Panic == "", "%s mode failed at %s: %s %s", mode, response.Stage, response.Error, response.Panic)
	var problems []string
	for _, problem := range fuzznative.DMLProblems(*model, response.DML) {
		problems = append(problems, mode+" mode: "+problem)
	}
	return append(problems, dmlReferenceProblems(ctx, t, suite, connection, model, mode)...)
}

// dmlPackages generates the version 17 catalogue for every mode.
//
// Takes t (*testing.T) which reports a refused catalogue.
// Takes suite (NativeFuzzSuite) which supplies the dialect and driver.
//
// Returns []modePackage which holds each mode's package.
func dmlPackages(t *testing.T, suite NativeFuzzSuite) []modePackage {
	t.Helper()
	packages := make([]modePackage, 0, len(executionModes(suite.Engine)))
	for _, mode := range executionModes(suite.Engine) {
		files, err := generateMode(t.Context(), suite, mode, fuzzcase.DMLSchema(suite.Engine), fuzzcase.DMLQueries(suite.Engine))
		require.NoError(t, err, "Seagull refuses the version 17 catalogue for the %s mode", mode)
		packages = append(packages, modePackage{mode: mode, files: files})
	}
	return packages
}

// dmlReferenceProblems reads the final table without generated code and compares the
// identifiers, quantities, notes, binary values and NULL states with the model.
//
// Takes t (*testing.T) which reports read failures.
// Takes suite (NativeFuzzSuite) which supplies the driver.
// Takes connection (string) which names the mode's database.
// Takes model (*fuzzcase.DMLCase) which supplies the expected final table.
// Takes mode (string) which names the mode for messages.
//
// Returns []string which lists the differences.
func dmlReferenceProblems(ctx context.Context, t *testing.T, suite NativeFuzzSuite, connection string, model *fuzzcase.DMLCase, mode string) []string {
	t.Helper()
	if len(model.Steps) == 0 {
		return nil
	}
	database, err := sql.Open(suite.Driver.Name, connection)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	text := cmp.Or(dmlQuantityText[suite.Engine], "CAST(qty AS TEXT)")
	// #nosec G202 -- The projection comes from a fixed per-engine choice.
	rows, err := database.QueryContext(ctx, "SELECT id, "+text+", note, note IS NULL, data, data IS NULL FROM "+fuzzcase.DMLTable+" ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	expected := model.Steps[len(model.Steps)-1].State
	var problems []string
	index := 0
	for ; rows.Next(); index++ {
		var reference dmlReferenceRow
		require.NoError(t, rows.Scan(&reference.id, &reference.quantity, &reference.note, &reference.noteNull, &reference.data, &reference.dataNull))
		if index >= len(expected) {
			problems = append(problems, fmt.Sprintf("%s mode reference: unexpected row %d", mode, reference.id))
			continue
		}
		if problem := reference.compare(expected[index]); problem != "" {
			problems = append(problems, fmt.Sprintf("%s mode reference row %d: %s", mode, reference.id, problem))
		}
	}
	require.NoError(t, rows.Err())
	if index < len(expected) {
		problems = append(problems, fmt.Sprintf("%s mode reference: %d rows, the model expects %d", mode, index, len(expected)))
	}
	return problems
}

// truth reads a driver's Boolean or integer predicate result.
//
// Takes value (any) which is the scanned predicate.
//
// Returns bool which is true for true or a non-zero integer.
func truth(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int64:
		return typed != 0
	case uint8:
		return typed != 0
	case []byte:
		return string(typed) == "1" || string(typed) == "true"
	default:
		text := fmt.Sprint(typed)
		return text == "1" || text == "true"
	}
}

// asBytes reads a driver's text or binary value.
//
// Takes value (any) which is the scanned value.
//
// Returns []byte which is empty for NULL.
func asBytes(value any) []byte {
	switch typed := value.(type) {
	case []byte:
		return typed
	case string:
		return []byte(typed)
	case *string:
		if typed != nil {
			return []byte(*typed)
		}
		return nil
	default:
		return nil
	}
}

// describeDML lists a sequence's statements for failure messages.
//
// Takes model (*fuzzcase.DMLCase) which holds the statements.
//
// Returns string which has one line per statement.
func describeDML(model *fuzzcase.DMLCase) string {
	var description strings.Builder
	for index := range model.Steps {
		step := &model.Steps[index]
		fmt.Fprintf(&description, "%d %s rows=%d key=%d value=%+v fails=%t affected=%d commit=%t\n", index, step.Operation, len(step.Rows), step.Key, step.Value,
			step.Fails, step.Affected, step.Commit)
		for position := range step.Inner {
			inner := &step.Inner[position]
			fmt.Fprintf(&description, "  %s rows=%d key=%d fails=%t affected=%d\n", inner.Operation, len(inner.Rows), inner.Key, inner.Fails, inner.Affected)
		}
	}
	return description.String()
}
