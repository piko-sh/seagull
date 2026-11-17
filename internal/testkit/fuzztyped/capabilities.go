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

package fuzztyped

import (
	"slices"
	"time"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// statusExcluded marks an excluded capability.
const statusExcluded = "excluded"

// RegressionMarker ends a saved reproduction of a finding. No finding about a value is
// open, so the decoder only removes it, and an input reads the same with or without it.
const RegressionMarker = "\x00sgl-regression"

// valueRule records a value an engine refuses or changes, which inputs skip or expect.
type valueRule struct {
	// feature names the values.
	feature string

	// reason explains the engine's or driver's behaviour.
	reason string

	// engines lists the configurations it applies to.
	engines []string
}

// valueRules lists the engine and driver behaviours the storage rules encode.
var valueRules = []valueRule{
	{feature: "value/float-nonfinite", engines: []string{engineMySQL, engineMariaDB},
		reason: "MySQL and MariaDB refuse NaN and the infinities in DOUBLE columns."},
	{feature: "value/float-nan-null", engines: []string{engineSQLite}, reason: "SQLite stores a NaN REAL as NULL."},
	{feature: "value/clickhouse-subsecond-before-epoch", engines: []string{engineClickHouse},
		reason: "ClickHouse reads a Unix time between -1 and 0 seconds, such as -0.000001, as after 1970, " +
			"and every other spelling depends on the column's time zone, so Seagull refuses such a timestamp."},
	{feature: "value/float-negative-zero", engines: []string{engineSQLite, engineMariaDB}, reason: "The engine stores negative zero as zero."},
	{feature: "value/float-equal-update", engines: []string{engineDuckDB},
		reason: "DuckDB leaves a float unchanged when an UPDATE writes an equal value, so zero over negative zero keeps negative zero."},
	{feature: "value/time-round", engines: []string{engineMySQL}, reason: "MySQL rounds fractional seconds to microseconds half up."},
	{feature: "value/text-nul", engines: postgresFamily, reason: "PostgreSQL and CockroachDB text cannot hold NUL."},
	{feature: "value/time-microseconds", engines: microsecondEngines,
		reason: "The engine stores microseconds and its Go driver drops the remaining nanoseconds toward the earlier instant."},
	{feature: "value/time-range", engines: []string{engineMySQL, engineMariaDB, engineClickHouse},
		reason: "DATETIME holds years 1000 to 9999, TIMESTAMP 1970 to 2038 and DateTime64 1900 to 2262."},
	{feature: "value/array-null", engines: []string{engineClickHouse}, reason: "ClickHouse arrays cannot be NULL. A nil slice is stored empty."},
	{feature: "value/array-null-member", engines: []string{engineDuckDB},
		reason: "duckdb-go cannot bind a list holding NULL (it panics on []*int64 and nil members), so Seagull types the parameter []int64."},
	{feature: "value/nested-null", engines: []string{engineClickHouse}, reason: "Array(Array(Int64)) members cannot be NULL."},
	{feature: "value/nested-ragged", engines: []string{enginePostgres, engineTimescale},
		reason: "PostgreSQL arrays are rectangular and read an array of empty arrays as one empty array."},
	{feature: "value/boolean-integer", engines: integerBooleanEngines, reason: "The engine returns comparisons as the integers zero and one."},
	{feature: "value/ascii-case", engines: asciiCaseEngines, reason: "lower and upper change only ASCII letters."},
	{feature: "value/like-ascii-fold", engines: []string{engineSQLite}, reason: "SQLite LIKE ignores the case of ASCII letters."},
}

// Capabilities lists the families, queries, modes and values an engine's inputs leave out
// or treat specially, each with its reason.
//
// Takes engine (string) which names the dialect.
//
// Returns []fuzzcase.Capability which records each exclusion.
func Capabilities(engine string) []fuzzcase.Capability {
	var result []fuzzcase.Capability
	add := func(feature, reason string) {
		result = append(result, fuzzcase.Capability{Feature: feature, NativeType: "", CastType: "", Status: statusExcluded, Reason: reason})
	}
	if !ModeSupported(engine, ModePrepared) {
		add("mode/prepared", "The ClickHouse database/sql driver prepares INSERT batches, not general queries.")
	}
	if !ModeSupported(engine, ModeNative) {
		add("mode/native", "This engine has no dedicated generated driver API.")
	}
	for index := range families {
		if reason, ok := families[index].excluded[engine]; ok {
			add("family/"+families[index].name, reason)
		}
	}
	for index := range expressions {
		if reason, ok := expressions[index].excluded[engine]; ok {
			add("query/"+expressions[index].name, reason)
		}
	}
	for _, rule := range valueRules {
		if slices.Contains(rule.engines, engine) {
			add(rule.feature, rule.reason)
		}
	}
	return result
}

// unsendableEpoch reports a value no input writes. That is a ClickHouse timestamp less
// than a second before 1970, which Seagull refuses to send, as the value-rule
// value/clickhouse-subsecond-before-epoch records.
//
// Takes engine (string) which names the dialect.
// Takes value (Value) which is a candidate value.
//
// Returns bool which is true for a timestamp less than a second before 1970 on
// ClickHouse.
func unsendableEpoch(engine string, value Value) bool {
	if engine != engineClickHouse || (value.Kind != KindTime && value.Kind != KindInstant) {
		return false
	}
	layout := time.RFC3339Nano
	if value.Kind == KindTime {
		layout = wallLayout
	}
	moment, err := time.Parse(layout, value.Text)
	return err == nil && moment.Unix() == -1 && moment.Nanosecond() != 0
}
