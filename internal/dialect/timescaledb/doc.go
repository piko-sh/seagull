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

// Package timescaledb is the dialect for TimescaleDB: the postgres dialect extended
// through its options and extension interface.
//
// TimescaleDB is a postgres extension that adds time-series functionality (hypertables,
// continuous aggregates, compression, retention policies, time_bucket / first / last /
// hyperfunctions). The vast majority of its SQL surface is plain postgres function calls;
// only a handful of DDL forms (CREATE HYPERTABLE, CREATE MATERIALIZED VIEW ... WITH
// (timescaledb.continuous = true), ALTER TABLE ... SET (timescaledb.compress = ...))
// require new parser behaviour.
//
// The package re-uses the postgres dialect through its extensibility hooks
// (WithExtraTypes, WithExtraFunctions, WithStatementExtensions, WithPostParseHook). It
// contributes TimescaleDB function signatures and opaque aggregate-state types
// (statssummary1d, counter_summary, candlestick, hyperloglog, tdigest, and similar).
//
// A StatementExtensionPort classifies and parses CREATE HYPERTABLE in keyword and
// function-call form, including by_range and by_hash dimension builders, CREATE
// MATERIALIZED VIEW with the timescaledb.continuous reloption, ALTER TABLE and ALTER
// MATERIALIZED VIEW SET (...) compression statements, the policy and job management calls
// (add_compression_policy, add_retention_policy, add_continuous_aggregate_policy,
// add_reorder_policy, add_columnstore_policy and their remove counterparts, plus add_job,
// alter_job, delete_job, run_job), the hypertable management calls (add_dimension,
// set_chunk_time_interval, set_integer_now_func, enable_chunk_skipping,
// disable_chunk_skipping), and the CALL refresh_continuous_aggregate procedure.
//
// A PostParseHook recognises the TS 2.18+ canonical CREATE TABLE foo (...) WITH
// (tsdb.hypertable, ...) form. The plain CREATE TABLE flows through the host postgres
// handler, which records the WITH body's storage parameters on the mutation; the hook
// lifts the recognised tsdb.* and timescaledb.* keys among them into
// CatalogueMutation.EngineSpecific. The keyword CREATE HYPERTABLE form reads its column
// and constraint list through the same postgres parsing as CREATE TABLE.
//
// TimescaleDB-specific metadata is attached to catalogue mutations via the EngineSpecific
// map with keys prefixed `TIMESCALE_`. The postgres engine does not introspect these
// keys; downstream consumers (catalogue browsers, codegen specialisers) read them when
// they want to surface "this is a hypertable" affordances. Policy and management calls
// additionally carry TIMESCALE_POLICY_OP=<canonical-op> so a single switch routes on
// intent without re-classifying the kind.
package timescaledb
