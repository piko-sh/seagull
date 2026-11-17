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

package fuzzsemantic

import (
	"slices"

	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// statusExcluded marks an excluded capability.
const statusExcluded = "excluded"

const (
	// featurePivot is the pivot containment relation.
	featurePivot = "relation/pivot"

	// featurePartitionRows is the row partition relation.
	featurePartitionRows = "relation/partition-rows"

	// featurePartitionAggregates is the aggregate partition relation.
	featurePartitionAggregates = "relation/partition-aggregates"

	// featureSubqueryCounts is the CTE and derived table count relation.
	featureSubqueryCounts = "relation/subquery-counts"

	// featureWindow is the window numbering and total relation.
	featureWindow = "relation/window"

	// featureWindowFilter is the relation between a filtered and an unfiltered window
	// builder.
	featureWindowFilter = "relation/window-filter"

	// featureLeftJoin is the LEFT JOIN count relation.
	featureLeftJoin = "relation/left-join"

	// featureFullJoin is the FULL JOIN count relation.
	featureFullJoin = "relation/full-join"

	// featureSemiJoin is the IN and DISTINCT join semi-join relation.
	featureSemiJoin = "relation/semi-join"

	// featureCorrelated is the EXISTS formulation of the semi-join.
	featureCorrelated = "relation/semi-join-exists"

	// featureUnionAll is the UNION ALL relation.
	featureUnionAll = "relation/union-all"

	// featureUnion is the UNION and DISTINCT relation.
	featureUnion = "relation/union"

	// featureSetOperations is the INTERSECT and EXCEPT relation.
	featureSetOperations = "relation/intersect-except"

	// featureStaticPages is the static LIMIT and OFFSET page relation.
	featureStaticPages = "relation/static-pages"

	// featureBuilderPages is the runtime builder page relation.
	featureBuilderPages = "relation/builder-pages"

	// featureBuilderLimit is the relation between a filtered and an unfiltered builder over
	// a query that limits its own rows.
	featureBuilderLimit = "relation/builder-limit"
)

// features lists every relation family in report order.
var features = []string{featurePivot, featurePartitionRows, featurePartitionAggregates, featureSubqueryCounts, featureWindow,
	featureWindowFilter, featureLeftJoin, featureFullJoin, featureSemiJoin, featureCorrelated, featureUnionAll, featureUnion,
	featureSetOperations, featureStaticPages, featureBuilderPages, featureBuilderLimit}

// engineExclusion records a relation an engine cannot express.
type engineExclusion struct {
	// feature names the relation.
	feature string

	// reason explains the exclusion.
	reason string

	// engines lists the configurations it applies to.
	engines []string
}

// engineExclusions lists the relations engines cannot express.
var engineExclusions = []engineExclusion{
	{feature: featureFullJoin, engines: []string{engineMySQL, engineMariaDB},
		reason: "Engine limitation: MySQL and MariaDB have no FULL JOIN."},
}

// Capabilities lists the modes and relations an engine's inputs leave out.
//
// Takes engine (string) which names the dialect.
//
// Returns []fuzzcase.Capability which records each exclusion with its reason.
func Capabilities(engine string) []fuzzcase.Capability {
	var result []fuzzcase.Capability
	if !fuzzbuilder.ModeSupported(engine, fuzzbuilder.ModePrepared) {
		result = append(result, capability("mode/prepared", "Driver limitation: the ClickHouse database/sql driver prepares INSERT batches, not general queries."))
	}
	if !fuzzbuilder.ModeSupported(engine, fuzzbuilder.ModeNative) {
		result = append(result, capability("mode/native", "Seagull scope: this engine has no dedicated generated driver API."))
	}
	for _, exclusion := range engineExclusions {
		if slices.Contains(exclusion.engines, engine) {
			result = append(result, capability(exclusion.feature, exclusion.reason))
		}
	}
	return result
}

// capability builds one exclusion record.
//
// Takes feature (string) which names the excluded feature.
// Takes reason (string) which explains it.
//
// Returns fuzzcase.Capability which records the exclusion.
func capability(feature, reason string) fuzzcase.Capability {
	return fuzzcase.Capability{Feature: feature, NativeType: "", CastType: "", Status: statusExcluded, Reason: reason}
}

// admits reports whether an engine can express a relation.
//
// Takes engine (string) which names the dialect.
// Takes feature (string) which names the relation.
//
// Returns bool which is false for an engine exclusion.
func admits(engine, feature string) bool {
	for _, exclusion := range engineExclusions {
		if exclusion.feature == feature && slices.Contains(exclusion.engines, engine) {
			return false
		}
	}
	return true
}

// enabled reports whether the case checks a relation.
//
// Takes feature (string) which names the relation.
//
// Returns bool which is false for an engine exclusion.
func (c *Case) enabled(feature string) bool {
	return admits(c.Engine, feature)
}

// Features lists the relations the case checks.
//
// Returns []string which holds the enabled relation families in report order.
func (c Case) Features() []string {
	var result []string
	for _, feature := range features {
		if c.enabled(feature) {
			result = append(result, feature)
		}
	}
	return result
}
