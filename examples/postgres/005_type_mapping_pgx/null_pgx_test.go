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

package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"seagull.sh/seagull/examples/postgres/005_type_mapping_pgx/internal/store"
)

// requireError fails the test unless err is an error whose message contains want.
func requireError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got error %v, want one containing %q", err, want)
	}
}

// requireNoError fails the test when err is not nil.
func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestNullScanFloat64RefusesAFloat32Overflow(t *testing.T) {
	t.Parallel()

	var narrow store.Null[float32]
	requireError(t, narrow.ScanFloat64(pgtype.Float8{Float64: math.MaxFloat64, Valid: true}), "overflows float32")
	requireNoError(t, narrow.ScanFloat64(pgtype.Float8{Float64: math.Inf(1), Valid: true}))
	if !math.IsInf(float64(narrow.V), 1) {
		t.Fatalf("got %v, want +Inf", narrow.V)
	}
	requireNoError(t, narrow.ScanFloat64(pgtype.Float8{Float64: 1.5, Valid: true}))
	if narrow != (store.Null[float32]{V: 1.5, Valid: true}) {
		t.Fatalf("got %+v, want 1.5", narrow)
	}
}

func TestNullScanInt64RefusesAnInt16Overflow(t *testing.T) {
	t.Parallel()

	var small store.Null[int16]
	requireError(t, small.ScanInt64(pgtype.Int8{Int64: math.MaxInt16 + 1, Valid: true}), "overflows int16")
	requireNoError(t, small.ScanInt64(pgtype.Int8{Int64: 0, Valid: false}))
	if small != (store.Null[int16]{V: 0, Valid: false}) {
		t.Fatalf("got %+v, want NULL", small)
	}
}

func TestNullScanIntervalRefusesADurationOverflow(t *testing.T) {
	t.Parallel()

	var duration store.Null[time.Duration]
	requireError(t, duration.ScanInterval(pgtype.Interval{Months: math.MaxInt32, Valid: true}), "overflows time.Duration")
	requireError(t, duration.ScanInterval(pgtype.Interval{Microseconds: math.MaxInt64, Valid: true}), "overflows time.Duration")
	requireNoError(t, duration.ScanInterval(pgtype.Interval{Months: 1, Days: 1, Microseconds: 1, Valid: true}))
	if want := 31*24*time.Hour + time.Microsecond; duration.V != want {
		t.Fatalf("got %v, want %v", duration.V, want)
	}
}
