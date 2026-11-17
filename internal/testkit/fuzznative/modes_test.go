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

package fuzznative

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

type canonicalDecimal struct{ digits string }

func (decimal *canonicalDecimal) String() string { return decimal.digits }

func TestCanonicalKeepsNullEmptyAndNilApartAndIgnoresGoWidths(t *testing.T) {
	t.Parallel()
	var nilBytes []byte
	var missing *int64
	require.Equal(t, Canonical(reflect.ValueOf(int32(5))), Canonical(reflect.ValueOf(int64(5))))
	require.NotEqual(t, Canonical(reflect.ValueOf([]byte{})), Canonical(reflect.ValueOf(nilBytes)))
	require.Equal(t, "null", Canonical(reflect.ValueOf(missing)))
	require.NotEqual(t, Canonical(reflect.ValueOf("")), Canonical(reflect.ValueOf(missing)))
	moment := time.Date(2000, 2, 29, 23, 59, 59, 999999000, time.UTC)
	require.Equal(t, "time:2000-02-29T23:59:59.999999Z", Canonical(reflect.ValueOf(moment)))
}

func TestCanonicalReadsPointerStringersFromUnaddressableRows(t *testing.T) {
	t.Parallel()
	type row struct{ Amount canonicalDecimal }
	value := row{Amount: canonicalDecimal{digits: "7.00"}}
	addressable := reflect.ValueOf(&value).Elem()
	require.Equal(t, CanonicalFields(addressable), CanonicalFields(reflect.ValueOf(value)))
	require.Equal(t, []string{`text:"7.00"`}, CanonicalFields(reflect.ValueOf(value)))
}

func TestDMLProblemsRejectsWrongCountsStatesAndOutcomes(t *testing.T) {
	t.Parallel()
	model, ok := fuzzcase.DecodeDML("mysql", []byte{fuzzcase.DMLVersion, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	require.True(t, ok)
	require.Len(t, model.Steps, 2)
	require.True(t, model.Steps[1].Fails)
	state := func(step *fuzzcase.DMLStep) []DMLObservedRow {
		rows := make([]DMLObservedRow, 0, len(step.State))
		for _, row := range step.State {
			rows = append(rows, DMLObservedRow{Values: row.Values, ID: row.ID})
		}
		return rows
	}
	correct := []DMLObservation{{State: state(&model.Steps[0])}, {Error: "duplicate", State: state(&model.Steps[1])}}
	require.Empty(t, DMLProblems(model, correct))

	accepted := []DMLObservation{correct[0], {State: state(&model.Steps[1])}}
	require.NotEmpty(t, DMLProblems(model, accepted), "a duplicate key the engine should refuse")

	empty := []DMLObservation{{State: nil}, correct[1]}
	require.NotEmpty(t, DMLProblems(model, empty), "a row missing after its insert")

	nulled := state(&model.Steps[0])
	nulled[0].Values = append([]fuzzcase.TypedValue(nil), nulled[0].Values...)
	nulled[0].Values[2] = fuzzcase.TypedValue{Kind: "binary", Null: true}
	require.NotEmpty(t, DMLProblems(model, []DMLObservation{{State: nulled}, correct[1]}), "a NOT NULL binary value read as NULL")

	update, ok := fuzzcase.DecodeDML("postgres", []byte{fuzzcase.DMLVersion, 0, 0, 0, 0, 0, 4, 0, 0})
	require.True(t, ok)
	wrong := []DMLObservation{{State: state(&update.Steps[0])}, {Affected: 0, Counted: true, State: state(&update.Steps[1])}}
	require.NotEmpty(t, DMLProblems(update, wrong), "PostgreSQL counts a matched row even when its value is unchanged")
}
