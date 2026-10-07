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

package timescaledb

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

var (
	// hypertableReloptionLifts maps a lower-cased `tsdb.*` or `timescaledb.*` reloption key
	// to the TIMESCALE_* EngineSpecific key it should populate.
	//
	// The key is found in a plain CREATE TABLE WITH body. The map is the modern (TS 2.18+)
	// counterpart to timescaleReloptionMap; the keys differ because the modern form sits on
	// a regular CREATE TABLE while the legacy compression keys appear on ALTER TABLE /
	// CREATE MATERIALIZED VIEW. The boolean `tsdb.hypertable` / `timescaledb.hypertable`
	// toggle key is omitted here because it is the discriminator that activates the lift;
	// the hook surfaces it as TIMESCALE_HYPERTABLE separately.
	hypertableReloptionLifts = map[string]string{
		"tsdb.partition_column":          "TIMESCALE_TIME_COLUMN",
		"tsdb.chunk_interval":            "TIMESCALE_CHUNK_TIME_INTERVAL",
		"tsdb.segmentby":                 "TIMESCALE_SEGMENTBY",
		"tsdb.orderby":                   "TIMESCALE_ORDERBY",
		"tsdb.enable_columnstore":        "TIMESCALE_COLUMNSTORE_ENABLED",
		"timescaledb.partition_column":   "TIMESCALE_TIME_COLUMN",
		"timescaledb.chunk_interval":     "TIMESCALE_CHUNK_TIME_INTERVAL",
		"timescaledb.segmentby":          "TIMESCALE_SEGMENTBY",
		"timescaledb.orderby":            "TIMESCALE_ORDERBY",
		"timescaledb.enable_columnstore": "TIMESCALE_COLUMNSTORE_ENABLED",
	}

	// hypertableActivatorKeys lists the lower-cased reloption keys whose presence in a
	// CREATE TABLE WITH body marks the table as a hypertable. Either of the two namespaces
	// is accepted because the timescaledb extension registers both as synonyms.
	hypertableActivatorKeys = map[string]struct{}{
		"tsdb.hypertable":        {},
		"timescaledb.hypertable": {},
	}
)

// timescaleCreateTableHypertableHook lifts TS 2.18+ hypertable reloptions from a plain
// `CREATE TABLE x (...) WITH (tsdb.hypertable, ...)` statement into structured
// EngineSpecific keys on the mutation.
//
// The postgres CREATE TABLE handler records the storage parameters of the statement's
// WITH (...) clause on the mutation, under pgext.EngineSpecificStorageParameterPrefix,
// and the hook reads them there. It only acts on a CREATE TABLE mutation. When the
// parameters include no hypertable activator (`tsdb.hypertable` or
// `timescaledb.hypertable`) the mutation is left untouched, so a plain `CREATE TABLE x
// (...) WITH (fillfactor=80)` passes through unmodified.
//
// Takes mutation (*ast.CatalogueMutation) which receives the lifted EngineSpecific
// entries when the parameters activate the hypertable.
//
// Returns error which is always nil, as required by the PostParseHook signature.
func timescaleCreateTableHypertableHook(
	_ pgext.ParserContext,
	_ pgext.StatementKind,
	mutation *ast.CatalogueMutation,
) error {
	if mutation == nil || mutation.Kind != ast.MutationCreateTable {
		return nil
	}
	reloptions := storageParameters(mutation)
	if !containsHypertableActivator(reloptions) {
		return nil
	}
	applyHypertableReloptions(mutation, reloptions)
	return nil
}

// storageParameters returns the storage parameters the postgres CREATE TABLE handler
// recorded on a mutation, keyed by their lower-cased names.
//
// Takes mutation (*ast.CatalogueMutation) which is the CREATE TABLE mutation.
//
// Returns map[string]string which maps each parameter to its value, or nil when the
// statement gave none.
func storageParameters(mutation *ast.CatalogueMutation) map[string]string {
	var parameters map[string]string
	for key, value := range mutation.EngineSpecific {
		name, isParameter := strings.CutPrefix(key, pgext.EngineSpecificStorageParameterPrefix)
		if !isParameter {
			continue
		}
		if parameters == nil {
			parameters = map[string]string{}
		}
		parameters[name] = value
	}
	return parameters
}

// containsHypertableActivator reports whether the parsed reloptions include a
// `tsdb.hypertable` or `timescaledb.hypertable` key. The value is ignored; the
// activator's presence alone toggles the lift.
//
// Takes reloptions (map[string]string) which is the parsed body.
//
// Returns bool which is true when the activator is present.
func containsHypertableActivator(reloptions map[string]string) bool {
	for key := range reloptions {
		if _, ok := hypertableActivatorKeys[key]; ok {
			return true
		}
	}
	return false
}

// applyHypertableReloptions copies recognised TS 2.18+ reloption keys onto the mutation's
// EngineSpecific map.
//
// The TIMESCALE_HYPERTABLE marker is always set when the activator was present;
// recognised keys map to their structured EngineSpecific names via
// hypertableReloptionLifts. Unrecognised entries are preserved verbatim under
// TIMESCALE_RELOPTION_<UPPER_KEY> so nothing is lost silently.
//
// Takes mutation (*ast.CatalogueMutation) which receives the lifted entries.
// Takes reloptions (map[string]string) which is the parsed body.
func applyHypertableReloptions(mutation *ast.CatalogueMutation, reloptions map[string]string) {
	if mutation.EngineSpecific == nil {
		mutation.EngineSpecific = map[string]string{}
	}
	mutation.EngineSpecific["TIMESCALE_HYPERTABLE"] = literalTrue
	for key, value := range reloptions {
		if _, isActivator := hypertableActivatorKeys[key]; isActivator {
			continue
		}
		if engineSpecificKey, ok := hypertableReloptionLifts[key]; ok {
			mutation.EngineSpecific[engineSpecificKey] = value
			continue
		}
		mutation.EngineSpecific["TIMESCALE_RELOPTION_"+strings.ToUpper(key)] = value
	}
}
