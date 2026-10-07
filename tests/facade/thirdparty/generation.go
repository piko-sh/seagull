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

package thirdparty

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_kit"
)

const (
	// defaultMaxBinds is the most values a statement binds on a dialect that sets no limit.
	defaultMaxBinds = 65535
)

// generation is one emission: the request, the names the helpers take, and every query
// rendered for database/sql.
type generation struct {
	// request is the request being emitted.
	request *emit.Request

	// helpers names the generated helpers.
	helpers helperNames

	// queries holds the rendered queries in planning order: by source file, then name.
	queries []*query
}

// newGeneration renders every query of a request.
//
// Takes request (*emit.Request) which is the request.
//
// Returns *generation which holds the rendered queries.
// Returns error when a query's template does not render.
func newGeneration(request *emit.Request) (*generation, error) {
	current := &generation{request: request, helpers: newHelperNames(&request.Style), queries: make([]*query, 0, len(request.Queries))}
	for index := range request.Queries {
		rendered, err := newQuery(request, index, "")
		if err != nil {
			return nil, err
		}
		current.queries = append(current.queries, rendered)
	}
	slices.SortStableFunc(current.queries, func(left, right *query) int {
		return cmp.Or(cmp.Compare(left.analysed.Filename, right.analysed.Filename), cmp.Compare(left.analysed.Name, right.analysed.Name))
	})
	if request.Style.QueryNames.Enabled {
		if err := current.labelSharedSQL(); err != nil {
			return nil, err
		}
	}
	return current, nil
}

// labelSharedSQL adds identifying comments to queries that share SQL text, including
// sortable queries with the same base SELECT. This lets the query-names lookup
// distinguish them instead of assigning every copy to the first query.
//
// Returns error when a template does not render.
func (current *generation) labelSharedSQL() error {
	texts := sharedTexts{exact: make(map[string]bool), stems: make(map[string]bool)}
	for position, rendered := range current.queries {
		shared := texts.claim(&rendered.rendered, firstEdit(rendered, &rendered.rendered))
		if rendered.count != nil {
			shared = texts.claim(rendered.count, firstEdit(rendered, rendered.count)) || shared
		}
		if !shared {
			continue
		}
		line := emit_kit.CommentLine(&current.request.Dialect, rendered.naming.Label)
		labelled, err := newQuery(current.request, slices.Index(current.request.Queries, rendered.analysed), line)
		if err != nil {
			return err
		}
		current.queries[position] = labelled
	}
	return nil
}

// files writes every file of the package, in a fixed order: the query files by name, then
// the models, the querier, the helpers, the query-names lookup and the interface of the
// query methods.
//
// Returns []emit.GeneratedFile which holds the files.
// Returns error when the context ends or a file does not format.
func (current *generation) files(ctx context.Context) ([]emit.GeneratedFile, error) {
	var files []emit.GeneratedFile
	for _, filename := range slices.Sorted(maps.Keys(current.request.Naming.Files)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := current.queryFile(filename)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	for _, write := range []func() (emit.GeneratedFile, bool, error){current.modelsFile, current.querierFile, current.helpersFile, current.queryNamesFile} {
		file, wanted, err := write()
		if err != nil {
			return nil, err
		}
		if wanted {
			files = append(files, file)
		}
	}
	return current.withInterface(files)
}

// queryFile writes the file holding one query file's queries.
//
// Takes filename (string) which is the query file, as AnalysedQuery.Filename holds it.
//
// Returns emit.GeneratedFile which is the file.
// Returns error when the file does not format.
func (current *generation) queryFile(filename string) (emit.GeneratedFile, error) {
	file := newSource(&current.request.Naming)
	for _, rendered := range current.queries {
		if rendered.analysed.Filename == filename {
			current.writeQuery(file, rendered)
		}
	}
	return file.generated(current.request.Naming.Files[filename], current.request)
}

// maxBinds returns the most values one statement binds: the dialect's limit, or a
// conservative default when it sets none.
//
// Returns int which is the limit.
func (current *generation) maxBinds() int {
	if current.request.Dialect.MaxParameters > 0 {
		return current.request.Dialect.MaxParameters
	}
	return defaultMaxBinds
}

// query is one analysed query with its names and its SQL rendered for database/sql.
type query struct {
	// analysed is the analysed query.
	analysed *emit.AnalysedQuery

	// naming holds the query's names from the plan.
	naming *emit.QueryNaming

	// count is the rendered count of a runtime builder that counts its rows, or nil.
	count *emit_kit.Rendered

	// row is the row template of a batch, or nil.
	row *emit_kit.Row

	// rendered is the query's SQL, its arguments and its marks.
	rendered emit_kit.Rendered

	// form is the placeholder form the SQL is rendered in.
	form dialect.PlaceholderSigil
}

// newQuery renders one query of a request: its template, its count template when it is a
// runtime builder that counts, and its row template when it is a batch.
//
// Takes request (*emit.Request) which is the request.
// Takes index (int) which is the query's index in Request.Queries and Naming.Queries.
// Takes prefix (string) which is written before the query's SQL, or is empty.
//
// Returns *query which is the rendered query.
// Returns error when a template does not render.
func newQuery(request *emit.Request, index int, prefix string) (*query, error) {
	analysed := request.Queries[index]
	renderer := newRenderer(request, analysed)
	renderer.Prefix = prefix
	rendered, err := emit_kit.Render(analysed.Template, renderer)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", analysed.Name, err)
	}
	current := &query{analysed: analysed, naming: &request.Naming.Queries[index], count: nil, row: nil, rendered: rendered, form: renderer.Form}
	if analysed.DynamicRuntime && analysed.CountTemplate != nil {
		counted, countError := emit_kit.Render(*analysed.CountTemplate, renderer)
		if countError != nil {
			return nil, fmt.Errorf("query %s: counting: %w", analysed.Name, countError)
		}
		current.count = &counted
	}
	if analysed.Command == emit.QueryCommandBatch {
		row, rowError := emit_kit.RowTemplate(analysed, renderer)
		if rowError != nil {
			return nil, rowError
		}
		current.row = &row
	}
	return current, nil
}

// sharedTexts holds the texts the query-names lookup names queries by.
type sharedTexts struct {
	// exact holds the SQL of the renderings seen so far.
	exact map[string]bool

	// stems holds the text before the first run-time edit of the renderings seen so far.
	stems map[string]bool
}

// claim records the texts a rendering is named by and reports whether either was recorded
// before.
//
// Takes rendering (*emit_kit.Rendered) which is the rendering.
// Takes edited (int) which is the offset of its first run-time edit, or zero for none.
//
// Returns bool which is true when an earlier rendering is named by the same text.
func (texts sharedTexts) claim(rendering *emit_kit.Rendered, edited int) bool {
	shared := texts.exact[rendering.SQL]
	texts.exact[rendering.SQL] = true
	if edited > 0 {
		stem := rendering.SQL[:edited]
		shared = shared || texts.stems[stem]
		texts.stems[stem] = true
	}
	return shared
}

// newRenderer configures the renderer for query execution, leaving slice expansion to
// generated code and sortable ORDER BY clauses to the caller. It applies the style's star
// policy and leaves columns unwrapped.
//
// Takes request (*emit.Request) which holds the dialect and the style.
// Takes analysed (*emit.AnalysedQuery) which is the query.
//
// Returns *emit_kit.Renderer which is the renderer.
func newRenderer(request *emit.Request, analysed *emit.AnalysedQuery) *emit_kit.Renderer {
	info := &request.Dialect
	renderer := &emit_kit.Renderer{}
	renderer.KeepStars = request.Style.KeepStars
	renderer.Query = analysed
	renderer.Quote = info.IdentifierQuote
	renderer.Lists = info.Lists
	renderer.Form = emit_kit.ExecutionForm(info, analysed)
	renderer.Placeholders = info.Placeholders
	renderer.ListMode = emit_kit.ListExpand
	if sortableParameter(analysed) >= 0 {
		renderer.Omit = emit_kit.NewSegmentSet(emit.SegmentOrderBy)
	}
	return renderer
}
