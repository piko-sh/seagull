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
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

const schema = `
CREATE TYPE mood AS ENUM ('calm', 'stormy');

CREATE TABLE voyages (
    id        INTEGER PRIMARY KEY,
    stops     TIMESTAMP[] NOT NULL,
    days      DATE[],
    times     TIME[],
    instants  TIMESTAMPTZ[],
    fine      TIMESTAMP_NS[],
    berth     STRUCT(arrived TIMESTAMP, day DATE),
    by_day    MAP(DATE, INTEGER),
    by_weight MAP(DOUBLE, VARCHAR),
    by_flag   MAP(BOOLEAN, INTEGER),
    legs      STRUCT(seen TIMESTAMP[], schedule MAP(DATE, TIMESTAMP))[],
    fees      DECIMAL(8, 2)[],
    spans     INTERVAL[],
    counts    BIGINT[],
    huge      HUGEINT[],
    blobs     BLOB[],
    moods     mood[],
    readings  DOUBLE[]
);

CREATE TABLE arrivals (
    id  INTEGER PRIMARY KEY,
    arrived TIMESTAMP NOT NULL,
    day DATE NOT NULL
);
`

const seed = `
INSERT INTO voyages VALUES (1,
    [TIMESTAMP '2024-01-01 10:00:00.5', NULL],
    [DATE '2024-01-01', DATE '2024-02-29'],
    [TIME '10:00:00.25'],
    [TIMESTAMPTZ '2024-07-01 10:00:00+00'],
    [TIMESTAMP_NS '2024-01-01 10:00:00.123456789'],
    {'arrived': TIMESTAMP '2024-01-01 10:00:00', 'day': DATE '2024-01-02'},
    MAP {DATE '2024-01-01': 1, DATE '2024-01-02': NULL},
    MAP {1.5: 'x', 0.1: 'y'},
    MAP {true: 1, false: 0},
    [{'seen': [TIMESTAMP '2024-01-01 10:00:00'], 'schedule': MAP {DATE '2024-01-01': TIMESTAMP '2024-01-01 11:00:00'}}],
    [1.50, -0.05],
    [INTERVAL 1 DAY + INTERVAL 90 MINUTE, INTERVAL 14 MONTH, INTERVAL '-1.5 seconds'],
    [9007199254740993, -1],
    [170141183460469231731687303715884105727],
    ['\x00\x01ab'::BLOB],
    ['calm', 'stormy'],
    ['nan'::DOUBLE, 'inf'::DOUBLE, 1e300]);
INSERT INTO voyages (id, stops) VALUES (2, []);
INSERT INTO voyages (id, stops, berth, legs) VALUES (3, [TIMESTAMP '2024-03-01 00:00:00'],
    {'arrived': NULL, 'day': DATE '2024-03-01'},
    [NULL, {'seen': NULL, 'schedule': NULL}]);
INSERT INTO arrivals VALUES
    (1, TIMESTAMP '2024-01-01 08:00:00', DATE '2024-01-01'),
    (2, TIMESTAMP '2024-01-02 09:00:00', DATE '2024-01-01'),
    (3, TIMESTAMP '2024-01-03 10:00:00', DATE '2024-01-03');
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err = conn.ExecContext(ctx, schema+seed); err != nil {
		return err
	}
	queries := db.New(conn)
	voyages, err := queries.ListVoyages(ctx)
	if err != nil {
		return fmt.Errorf("ListVoyages: %w", err)
	}
	stops, err := queries.VoyageStops(ctx)
	if err != nil {
		return fmt.Errorf("VoyageStops: %w", err)
	}
	summary, err := queries.ArrivalSummary(ctx)
	if err != nil {
		return fmt.Errorf("ArrivalSummary: %w", err)
	}
	maps, err := queries.ArrivalMaps(ctx)
	if err != nil {
		return fmt.Errorf("ArrivalMaps: %w", err)
	}
	first := voyages[0]
	goTypes := []string{
		fmt.Sprintf("%T", first.Stops), fmt.Sprintf("%T", first.Berth), fmt.Sprintf("%T", first.ByDay),
		fmt.Sprintf("%T", first.ByWeight), fmt.Sprintf("%T", first.ByFlag), fmt.Sprintf("%T", first.Legs),
		fmt.Sprintf("%T", first.Fees), fmt.Sprintf("%T", first.Huge), fmt.Sprintf("%T", summary.Quartiles),
		fmt.Sprintf("%T", summary.PerDay), fmt.Sprintf("%T", maps[0].ArrivedByDay),
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"voyages":         describe(reflect.ValueOf(voyages)),
		"voyage_stops":    describe(reflect.ValueOf(stops)),
		"arrival_summary": describe(reflect.ValueOf(summary)),
		"arrival_maps":    describe(reflect.ValueOf(maps)),
		"go_types":        goTypes,
	})
}

func describe(value reflect.Value) any {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		if text, ok := describeLeaf(value); ok {
			return text
		}
		return describe(value.Elem())
	}
	if text, ok := describeLeaf(value); ok {
		return text
	}
	switch value.Kind() {
	case reflect.Slice:
		if value.IsNil() {
			return nil
		}
		described := make([]any, value.Len())
		for index := range value.Len() {
			described[index] = describe(value.Index(index))
		}
		return described
	case reflect.Map:
		return describeMap(value)
	case reflect.Struct:
		return describeStruct(value)
	default:
		return fmt.Sprint(value.Interface())
	}
}

func describeLeaf(value reflect.Value) (string, bool) {
	switch typed := value.Interface().(type) {
	case *time.Time:
		return typed.Format(time.RFC3339Nano), true
	case time.Time:
		return typed.Format(time.RFC3339Nano), true
	case []byte:
		return "0x" + hex.EncodeToString(typed), true
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), true
	case fmt.Stringer:
		return typed.String(), true
	}
	if value.Kind() == reflect.Struct && value.CanAddr() {
		if stringer, ok := value.Addr().Interface().(fmt.Stringer); ok {
			return stringer.String(), true
		}
	}
	return "", false
}

func describeMap(value reflect.Value) any {
	if value.IsNil() {
		return nil
	}
	described := make(map[string]any, value.Len())
	entries := value.MapRange()
	for entries.Next() {
		described[fmt.Sprint(describe(entries.Key()))] = describe(entries.Value())
	}
	return described
}

func describeStruct(value reflect.Value) any {
	described := make(map[string]any, value.NumField())
	for index := range value.NumField() {
		field := value.Type().Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" {
			name = field.Name
		}
		described[name] = describe(value.Field(index))
	}
	return described
}
