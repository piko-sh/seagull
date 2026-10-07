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

package pgx

import (
	"context"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/generate"
	"seagull.sh/seagull/internal/preset"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

func testRequest(packageName string, queries []*sqlmodel.AnalysedQuery, mappings sqlmodel.GoTypeResolver) *emit.Request {
	settings := preset.DefaultSettings()
	return &emit.Request{
		Catalogue:         nil,
		Types:             mappings,
		Package:           packageName,
		Dialect:           dialect.Wrap(postgres.New()).Info(),
		Queries:           queries,
		DirectivePrefixes: settings.Prefixes.Accepted(),
		Style:             settings.Style,
	}
}

func emitQueriesFor(ctx context.Context, emitter *Emitter, packageName string, queries []*sqlmodel.AnalysedQuery, mappings sqlmodel.GoTypeResolver) ([]sqlmodel.GeneratedFile, error) {
	request := planned(testRequest(packageName, queries, mappings))
	request.Style.Header = ""
	files, err := generate.Package(ctx, request, pgxDriver(request))
	if err != nil {
		return nil, err
	}
	kept := make([]sqlmodel.GeneratedFile, 0, len(files))
	for _, file := range files {
		if file.Name != "querier.go" && file.Name != "models.go" && !strings.HasSuffix(file.Name, "_json.go") {
			kept = append(kept, file)
		}
	}
	return kept, nil
}

func planned(request *emit.Request) *emit.Request {
	for _, query := range request.Queries {
		withTemplate(&request.Dialect, query)
	}
	request.Naming, _ = emitkit.PlanPackage(request, New().Reserve(&request.Dialect, &request.Style), emitkit.StyleNames(&request.Style))
	return request
}

func withTemplate(info *dialect.Facts, query *sqlmodel.AnalysedQuery) *sqlmodel.AnalysedQuery {
	if len(query.Template.Segments) > 0 || query.SQL == "" {
		return query
	}
	tokens, err := sqllex.Tokens(info.Lexical, query.SQL)
	if err != nil {
		return query
	}
	var segments []sqlmodel.SQLSegment
	cursor, ordinal := 0, 0
	for _, token := range tokens {
		if token.Kind != sqllex.KindParameter {
			continue
		}
		ordinal++
		index, typeText := fixtureParameter(query, token.Raw, ordinal)
		if index < 0 {
			continue
		}
		before := query.SQL[cursor:token.Start]
		segment := sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index, TypeText: typeText}
		end := token.End
		upper := strings.ToUpper(strings.TrimRight(before, " "))
		if query.Parameters[index].IsSlice && strings.HasSuffix(upper, "IN (") && strings.HasPrefix(query.SQL[token.End:], ")") {
			operatorStart := len(strings.TrimRight(before, " ")) - len("IN (")
			negated := strings.HasSuffix(strings.TrimRight(upper[:operatorStart], " "), "NOT")
			if negated {
				operatorStart = len(strings.TrimRight(before[:operatorStart], " ")) - len("NOT")
			}
			segment = sqlmodel.SQLSegment{Kind: sqlmodel.SegmentList, Parameter: index, TypeText: typeText, Negated: negated,
				Text: strings.TrimSuffix(before[operatorStart:], "(")}
			before = before[:operatorStart]
			end++
		}
		segments = append(segments, sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: before}, segment)
		cursor = end
	}
	segments = append(segments, sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: query.SQL[cursor:]})
	query.Template = sqlmodel.SQLTemplate{Segments: segments}
	return query
}

func fixtureParameter(query *sqlmodel.AnalysedQuery, raw string, ordinal int) (int, string) {
	name, typeText, _ := strings.Cut(strings.Trim(raw, "{}:@$?"), ":")
	digits := strings.TrimLeft(raw, "$?@p")
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		switch {
		case name != "" && parameter.Name == name:
			return index, typeText
		case digits != "" && strings.Trim(digits, "0123456789") == "" && strconv.Itoa(parameter.Number) == digits:
			return index, ""
		case raw == "?" && parameter.Number == ordinal:
			return index, ""
		}
	}
	if raw == "?" && ordinal <= len(query.Parameters) {
		return ordinal - 1, ""
	}
	return -1, ""
}
