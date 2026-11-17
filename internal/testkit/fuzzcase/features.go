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

package fuzzcase

import "slices"

// familyFeaturePrefix identifies fixed query families in audit reports.
const familyFeaturePrefix = "family/"

// expressionFeaturePrefix identifies grammar expression operations in audit reports.
const expressionFeaturePrefix = "expression/"

// compositionFeaturePrefix identifies grammar relations and modifiers in audit reports.
const compositionFeaturePrefix = "composition/"

// engineCapabilities records explicitly admitted engine extensions.
type engineCapabilities struct {
	// writes enables UPDATE and DELETE in the model.
	writes bool

	// expansion enables projection expressions that return several rows.
	expansion bool

	// qualify enables filtering after window evaluation.
	qualify bool

	// locking enables row locks after pagination.
	locking bool

	// settings enables a trailing settings clause.
	settings bool
}

// capabilities is the fixed engine capability table for structured inputs.
var capabilities = map[string]engineCapabilities{
	enginePostgres:   {locking: true, writes: true, expansion: true, qualify: false, settings: false},
	engineSQLite:     {locking: false, writes: true, expansion: false, qualify: false, settings: false},
	engineMySQL:      {locking: true, writes: true, expansion: false, qualify: false, settings: false},
	engineMariaDB:    {locking: true, writes: true, expansion: false, qualify: false, settings: false},
	engineDuckDB:     {locking: false, writes: true, expansion: true, qualify: true, settings: false},
	engineClickHouse: {locking: false, writes: false, expansion: true, qualify: true, settings: true},
	engineCockroach:  {locking: true, writes: true, expansion: false, qualify: false, settings: false},
	engineTimescale:  {locking: true, writes: true, expansion: true, qualify: false, settings: false},
}

// shapeFeatures names each permanently assigned version one shape.
var shapeFeatures = [...]string{
	"select", "boolean", "distinct", "grouping", "aggregate", "left-join", "inner-join", "window", "compound", "cte",
	"correlated", "in-list", "case", "star", "insert", "update", "delete", "having", "derived", "set-returning",
	"qualify", "settings", "coalesce", "not-in-list",
}

// grammarSources names the version two relation operations, indexed by shape.
var grammarSources = [...]string{"table", "left-join", "inner-join", "cte", "derived"}

// grammarModifiers names the version two row-shaping flags.
var grammarModifiers = []struct {
	// feature names the modifier in audit reports.
	feature string
	// flag is the bit that enables the modifier.
	flag byte
}{
	{flag: 1, feature: "pagination"}, {flag: grammarDistinct, feature: "distinct"},
	{flag: grammarGrouped, feature: "grouping"}, {flag: grammarWindow, feature: "window"},
}

// expressionFeatures names every enabled composable expression operation.
var expressionFeatures = []string{
	"column", "literal", "parameter", "+", "-", "coalesce", "case", "negate", "true",
	"=", "<>", ">", ">=", "<", "<=", "AND", "OR", "NOT", "IS NULL", "IS NOT NULL", "IN", "NOT IN",
}

// Features records operations present in the independently decoded model.
//
// Returns []string which names the features exercised by this input.
func (m Model) Features() []string {
	if m.at(0) == valueVersion {
		return []string{familyFeaturePrefix + valueFeatures[m.Shape-valueShapeStart]}
	}
	if m.at(0) == extendedVersion {
		return []string{familyFeaturePrefix + extendedFeatures[m.Shape-extendedShapeStart]}
	}
	if m.at(0) == Version {
		return []string{familyFeaturePrefix + shapeFeatures[m.Shape]}
	}
	features := []string{compositionFeaturePrefix + grammarSources[m.Shape]}
	for _, modifier := range grammarModifiers {
		if m.modifierApplies(modifier.flag) {
			features = append(features, compositionFeaturePrefix+modifier.feature)
		}
	}
	projections, predicate := m.grammarExpressions()
	for _, node := range append(projections, predicate) {
		features = appendExpressionFeatures(features, node)
	}
	slices.Sort(features)
	return slices.Compact(features)
}

// modifierApplies reports whether a row-shaping flag takes effect in the query.
//
// Takes flag (byte) which is one modifier bit.
//
// Returns bool which is false for a window that grouping replaces.
func (m Model) modifierApplies(flag byte) bool {
	if m.Flags&flag == 0 {
		return false
	}
	return flag != grammarWindow || m.Flags&grammarGrouped == 0
}

// RequiredFeatures lists the independent coverage obligations for an engine.
//
// Takes engine (string) which selects the capability table entry.
//
// Returns []string which names all required positive seed features.
func RequiredFeatures(engine string) []string {
	if _, ok := capabilities[engine]; !ok {
		return nil
	}
	var features []string
	for shape, feature := range shapeFeatures {
		if supportsShape(engine, shape) {
			features = append(features, familyFeaturePrefix+feature)
		}
	}
	for index, feature := range extendedFeatures {
		if supportsShape(engine, extendedShapeStart+index) {
			features = append(features, familyFeaturePrefix+feature)
		}
	}
	for index, feature := range valueFeatures {
		if supportsShape(engine, valueShapeStart+index) {
			features = append(features, familyFeaturePrefix+feature)
		}
	}
	for _, feature := range expressionFeatures {
		features = append(features, expressionFeaturePrefix+feature)
	}
	for _, feature := range grammarSources {
		features = append(features, compositionFeaturePrefix+feature)
	}
	for _, modifier := range grammarModifiers {
		features = append(features, compositionFeaturePrefix+modifier.feature)
	}
	slices.Sort(features)
	return features
}

// appendExpressionFeatures records every operation in an expression tree.
//
// Takes features ([]string) which accumulates coverage.
// Takes node (expression) which owns typed children.
//
// Returns []string which includes the node and its descendants.
func appendExpressionFeatures(features []string, node expression) []string {
	features = append(features, expressionFeaturePrefix+node.operation)
	for _, child := range node.children {
		features = appendExpressionFeatures(features, child)
	}
	return features
}
