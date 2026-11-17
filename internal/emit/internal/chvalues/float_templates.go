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

package chvalues

import (
	"slices"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// FloatParameters copies templates that reconstruct floating-point parameters exactly,
// for the database/sql driver.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which already have typed array lists.
// Takes transport (FloatTransport) which names the native bit types, or leaves queries
// unchanged when disabled.
//
// Returns []*sqlmodel.AnalysedQuery which keeps caller-owned analyses unchanged.
func FloatParameters(queries []*sqlmodel.AnalysedQuery, transport FloatTransport) []*sqlmodel.AnalysedQuery {
	return floatParameters(queries, transport, func(*sqlmodel.AnalysedQuery) bool { return false })
}

// NativeFloatParameters copies templates that reconstruct floating-point parameters
// exactly, for the native ClickHouse driver. It keeps columnar batch templates as they
// are.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which already have typed array lists.
// Takes transport (FloatTransport) which names the native bit types, or leaves queries
// unchanged when disabled.
//
// Returns []*sqlmodel.AnalysedQuery which keeps caller-owned analyses unchanged.
func NativeFloatParameters(queries []*sqlmodel.AnalysedQuery, transport FloatTransport) []*sqlmodel.AnalysedQuery {
	return floatParameters(queries, transport, nativeFloatBatch)
}

// floatParameters copies the templates of the queries with a floating-point parameter so
// they reconstruct its value exactly.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which already have typed array lists.
// Takes transport (FloatTransport) which names the native bit types.
// Takes keeps (func(*sqlmodel.AnalysedQuery) bool) which reports a query left as it is.
//
// Returns []*sqlmodel.AnalysedQuery which keeps caller-owned analyses unchanged.
func floatParameters(queries []*sqlmodel.AnalysedQuery, transport FloatTransport, keeps func(*sqlmodel.AnalysedQuery) bool) []*sqlmodel.AnalysedQuery {
	if !transport.Enabled() {
		return queries
	}
	var changed []*sqlmodel.AnalysedQuery
	for index, query := range queries {
		if keeps(query) {
			continue
		}
		hasFloat := slices.ContainsFunc(query.Parameters, func(parameter sqlmodel.QueryParameter) bool {
			return holdsFloatValue(&parameter.SQLType)
		})
		if !hasFloat {
			continue
		}
		if changed == nil {
			changed = append([]*sqlmodel.AnalysedQuery(nil), queries...)
		}
		copied := *query
		copied.Template.Segments = floatSegments(query.Template.Segments, query.Parameters, transport)
		rewriteOptionalTemplates(&copied, func(segments []sqlmodel.SQLSegment) []sqlmodel.SQLSegment {
			return floatSegments(segments, query.Parameters, transport)
		})
		changed[index] = &copied
	}
	if changed == nil {
		return queries
	}
	return changed
}

// floatSegments wraps placeholders while retaining structural clause and row segments.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the original template parts.
// Takes parameters ([]sqlmodel.QueryParameter) which describe native parameter types.
// Takes transport (FloatTransport) which names the native bit types.
//
// Returns []sqlmodel.SQLSegment which keeps values rows usable by batch generation.
func floatSegments(segments []sqlmodel.SQLSegment, parameters []sqlmodel.QueryParameter, transport FloatTransport) []sqlmodel.SQLSegment {
	if segments == nil {
		return nil
	}
	result := make([]sqlmodel.SQLSegment, 0, len(segments))
	for index := range segments {
		segment := segments[index]
		segment.Children = floatSegments(segment.Children, parameters, transport)
		if !bindsFloat(&segment, parameters) {
			result = append(result, segment)
			continue
		}
		wire, wrap := floatCast(&parameters[segment.Parameter], segment.TypeText, transport)
		if segment.Kind == sqlmodel.SegmentList {
			wrap.Before = segment.Text + "(" + wrap.Before
			wrap.After += ")"
			segment.Kind = sqlmodel.SegmentParameter
			segment.Text = ""
			segment.Negated = false
		}
		segment.TypeText = wire
		before, after := sqlmodel.SQLSegment{}, sqlmodel.SQLSegment{}
		before.Kind, before.Text = sqlmodel.SegmentText, wrap.Before
		after.Kind, after.Text = sqlmodel.SegmentText, wrap.After
		result = append(result, before, segment, after)
	}
	return result
}

// bindsFloat reports whether a segment binds a parameter holding floating-point values.
//
// Takes segment (*sqlmodel.SQLSegment) which is the template part.
// Takes parameters ([]sqlmodel.QueryParameter) which describe native parameter types.
//
// Returns bool which is true for a placeholder or list of a float parameter.
func bindsFloat(segment *sqlmodel.SQLSegment, parameters []sqlmodel.QueryParameter) bool {
	placeholder := segment.Kind == sqlmodel.SegmentParameter || segment.Kind == sqlmodel.SegmentList
	known := segment.Parameter >= 0 && segment.Parameter < len(parameters)
	return placeholder && known && holdsFloatValue(&parameters[segment.Parameter].SQLType)
}

// nativeFloatBatch identifies queries that send values through the columnar protocol.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the original query template.
//
// Returns bool which preserves the direct native binary representation.
func nativeFloatBatch(query *sqlmodel.AnalysedQuery) bool {
	if query.Command == sqlmodel.QueryCommandCopyFrom {
		return true
	}
	if query.Command != sqlmodel.QueryCommandBatch {
		return false
	}
	_, columns := emitkit.RowColumns(query)
	return columns
}
