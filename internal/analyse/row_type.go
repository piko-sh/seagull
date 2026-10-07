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

package analyse

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
)

// applyRowType records the model: or row: request for a query's row struct. It reports
// Q065 when the named table is missing, the query returns no rows or the query embeds
// tables.
//
// Takes query (*sqlmodel.AnalysedQuery) which receives the request.
// Takes directives (*directive.QueryDirectives) which holds the header's keywords.
// Takes directiveBlock (*directive.DirectiveBlock) which locates them.
// Takes filename (string) which is the query file, for diagnostics.
//
// Returns []diag.SourceError which holds the requests that cannot be honoured.
func (a *QueryAnalyser) applyRowType(
	query *sqlmodel.AnalysedQuery,
	directives *directive.QueryDirectives,
	directiveBlock *directive.DirectiveBlock,
	filename string,
) []diag.SourceError {
	keyword, value := directive.KeywordModel, directives.Model
	if value == "" {
		keyword, value = directive.KeywordRow, directives.Row
	}
	if value == "" {
		return nil
	}
	span := keywordValueSpan(directiveBlock, keyword)
	errorBuilder := diagbuild.NewErrorBuilder(filename)
	if problem := rowTypeProblem(query, keyword); problem != "" {
		return []diag.SourceError{errorBuilder.At(span, diag.CodeRowType, problem)}
	}
	if keyword == directive.KeywordRow {
		query.RowType = value
		return nil
	}
	table, problem := a.modelTable(value)
	if problem != "" {
		return []diag.SourceError{errorBuilder.At(span, diag.CodeRowType, problem)}
	}
	query.RowModel = sqlmodel.TableName{Schema: table.Schema, Name: table.Name}
	return nil
}

// modelTable finds the table a model: keyword names, optionally schema-qualified, whose
// model stands for it: the table itself, or for a partition the table it partitions.
//
// Takes written (string) which is the table as the keyword writes it.
//
// Returns *sqlmodel.Table which is the table, or nil when there is none.
// Returns string which describes why there is none, or is empty.
func (a *QueryAnalyser) modelTable(written string) (*sqlmodel.Table, string) {
	schema, name := "", written
	if before, after, found := strings.CutLast(written, "."); found && before != "" {
		schema, name = before, after
	}
	identifiers := a.catalogue.Identifiers
	schema, name = identifiers.Fold(schema, false), identifiers.Fold(name, false)
	table := a.catalogue.LookupTable(schema, name)
	if table != nil && table.PartitionOf != nil {
		table = a.catalogue.LookupTable(table.PartitionOf.Schema, table.PartitionOf.Name)
	}
	switch {
	case table != nil:
		return table, ""
	case a.catalogue.LookupView(schema, name) != nil:
		return nil, fmt.Sprintf("model: %s is a view, and only a table has a model; name a table, or name a shared row type with row:", written)
	default:
		return nil, fmt.Sprintf("model: the schema holds no table %s", written)
	}
}

// rowTypeProblem says why a query's rows cannot be read into a struct a keyword names:
// the query returns no rows, or embeds a table.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes keyword (string) which is the keyword, model or row.
//
// Returns string which describes the problem, or is empty when there is none.
func rowTypeProblem(query *sqlmodel.AnalysedQuery, keyword string) string {
	switch query.Command {
	case sqlmodel.QueryCommandOne, sqlmodel.QueryCommandMany, sqlmodel.QueryCommandStream:
	default:
		if !query.DynamicRuntime {
			return fmt.Sprintf("%s: names the struct a query's rows are read into, and the %s command returns no rows", keyword, query.Command)
		}
	}
	for index := range query.OutputColumns {
		if query.OutputColumns[index].IsEmbedded {
			return fmt.Sprintf("%s: cannot shape the rows of a query that embeds a table, which hold a struct for each embed; "+
				"drop %s: or the embed", keyword, keyword)
		}
	}
	return ""
}

// keywordValueSpan locates the value of a query header's keyword.
//
// Takes directiveBlock (*directive.DirectiveBlock) which holds the header's keywords.
// Takes keyword (string) which is the keyword.
//
// Returns diagbuild.TextSpan which is the value's span, or the header's when the keyword
// is not found.
func keywordValueSpan(directiveBlock *directive.DirectiveBlock, keyword string) diagbuild.TextSpan {
	for _, metadata := range directiveBlock.Metadata {
		if metadata.Directive == keyword {
			return metadata.ValueSpan
		}
	}
	return directiveBlock.Span
}
