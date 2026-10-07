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

package session

import (
	"slices"
	"strings"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// sentFromGenerated marks SQL read from the constant the generated method sends.
	sentFromGenerated = "generated"

	// sentAsWritten marks SQL shown as written, because no generated constant was found.
	sentAsWritten = "as written"

	// unknownName names an enum value the tables below do not list.
	unknownName = "unknown"

	// rowKindOwn marks rows read into the query's own row type.
	rowKindOwn = "own"

	// rowKindModel marks rows read into a table's model.
	rowKindModel = "model"

	// rowKindShared marks rows read into a row type queries share.
	rowKindShared = "shared"
)

var (
	// commandNames names each query command as the sgl.query directive spells it. It is the
	// table sgl describe prints with.
	commandNames = map[emit.QueryCommand]string{
		emit.QueryCommandOne:        "one",
		emit.QueryCommandMany:       "many",
		emit.QueryCommandExec:       "exec",
		emit.QueryCommandExecResult: "execresult",
		emit.QueryCommandExecRows:   "execrows",
		emit.QueryCommandBatch:      "batch",
		emit.QueryCommandStream:     "stream",
		emit.QueryCommandCopyFrom:   "copyfrom",
		emit.QueryCommandAsyncExec:  "asyncexec",
	}

	// categoryNames names each SQL type category in engine-neutral words.
	categoryNames = map[dialect.SQLTypeCategory]string{
		dialect.TypeCategoryInteger:        "integer",
		dialect.TypeCategoryFloat:          "float",
		dialect.TypeCategoryDecimal:        "decimal",
		dialect.TypeCategoryBoolean:        "boolean",
		dialect.TypeCategoryText:           "text",
		dialect.TypeCategoryBinary:         "binary",
		dialect.TypeCategoryTemporal:       "temporal",
		dialect.TypeCategoryJSON:           "json",
		dialect.TypeCategoryUUID:           "uuid",
		dialect.TypeCategoryNetwork:        "network",
		dialect.TypeCategoryGeometric:      "geometric",
		dialect.TypeCategoryEnum:           "enum",
		dialect.TypeCategoryComposite:      "composite",
		dialect.TypeCategoryArray:          "array",
		dialect.TypeCategoryRange:          "range",
		dialect.TypeCategoryStruct:         "struct",
		dialect.TypeCategoryMap:            "map",
		dialect.TypeCategoryUnion:          "union",
		dialect.TypeCategoryUnknown:        "unknown",
		dialect.TypeCategoryAggregateState: "aggregate-state",
	}

	// emptyResponse is a response with every slice empty, describing a run that did not
	// complete. Callers copy it by assignment; its slices have no capacity, so appending to
	// a copy never writes into the shared value.
	emptyResponse = Response{
		Config:      nil,
		Catalogue:   nil,
		Error:       "",
		Stages:      []StageView{},
		Files:       []FileView{},
		Diagnostics: []DiagnosticView{},
		Queries:     []QueryView{},
		Counts:      Counts{Errors: 0, Warnings: 0, Hints: 0},
		DurationMS:  0,
		V:           ProtocolVersion,
		OK:          false,
		Partial:     false,
		Internal:    false,
	}
)

// Response is the outcome of one run, shaped for the playground. Every slice is empty
// rather than nil, so it encodes as [].
type Response struct {
	// Config describes the configuration the run used, or is nil when the run failed before
	// the configuration was read.
	Config *ConfigView `json:"config"`

	// Catalogue describes the schema the migrations build, or is nil when the run did not
	// complete.
	Catalogue *CatalogueView `json:"catalogue"`

	// Error describes why the run did not complete, or is empty.
	Error string `json:"error"`

	// Stages holds the time each stage of the generation took.
	Stages []StageView `json:"stages"`

	// Files holds the generated files. Error diagnostics leave it empty unless a run that
	// skips failing queries produces partial output.
	Files []FileView `json:"files"`

	// Diagnostics holds every diagnostic, in source order.
	Diagnostics []DiagnosticView `json:"diagnostics"`

	// Queries holds the analysed queries, in file order.
	Queries []QueryView `json:"queries"`

	// Counts counts the diagnostics by severity.
	Counts Counts `json:"counts"`

	// DurationMS is how long the whole run took, in milliseconds.
	DurationMS float64 `json:"durationMs"`

	// V is the protocol version.
	V int `json:"v"`

	// OK reports that the run completed. Counts.Errors reports error diagnostics, and
	// Partial identifies output generated despite errors in some queries.
	OK bool `json:"ok"`

	// Partial reports that Files leave out at least one query with an error of its own, as a
	// run that skips failing queries does; those queries are marked Skipped, and
	// Counts.Errors still counts their errors.
	Partial bool `json:"partial"`

	// Internal reports that the run failed because of a fault in the playground or the
	// generator rather than in the project.
	Internal bool `json:"internal"`
}

// ConfigView describes the configuration a run used.
type ConfigView struct {
	// StructTags is go.struct_tags, the struct tags every generated field holding a column
	// carries besides json.
	StructTags map[string]string `json:"structTags"`

	// PackageNames is go.types.package_names, the names of the packages whose import path
	// does not imply them.
	PackageNames map[string]string `json:"packageNames"`

	// GroupByUseNulls is dialect_options.group_by_use_nulls, or nil for the server's
	// default.
	GroupByUseNulls *bool `json:"groupByUseNulls"`

	// ANSIQuotes is dialect_options.ansi_quotes, or nil for the server's default.
	ANSIQuotes *bool `json:"ansiQuotes"`

	// File is the configuration file's path, or empty when the project has none and sgl's
	// defaults were used.
	File string `json:"file"`

	// Database names the database generated.
	Database string `json:"database"`

	// Dialect names the dialect.
	Dialect string `json:"dialect"`

	// Driver names the driver API the generated code targets.
	Driver string `json:"driver"`

	// Package is the generated package's name.
	Package string `json:"package"`

	// Migrations is the migration directory.
	Migrations string `json:"migrations"`

	// Queries is the query directory.
	Queries string `json:"queries"`

	// InterfaceName is go.interface.name, the name of the interface of the query methods.
	InterfaceName string `json:"interfaceName"`

	// JSONCase is go.json_tags.case: snake, camel, pascal, or empty for the names as
	// written.
	JSONCase string `json:"jsonCase"`

	// Warnings holds the configuration the playground cannot honour.
	Warnings []string `json:"warnings"`

	// SearchPath is dialect_options.search_path, empty for the dialect's default.
	SearchPath []string `json:"searchPath"`

	// Prepared reports whether the generated code caches prepared statements.
	Prepared bool `json:"prepared"`

	// SkipFailingQueries reports whether the run generates the queries without an error of
	// their own and leaves out those with one, as sgl generate does.
	SkipFailingQueries bool `json:"skipFailingQueries"`

	// NonNullElements is go.types.non_null_elements.
	NonNullElements bool `json:"nonNullElements"`

	// KeepStars is go.keep_stars.
	KeepStars bool `json:"keepStars"`

	// Interface is go.interface.enabled.
	Interface bool `json:"interface"`

	// JSONOmitEmpty is go.json_tags.omitempty.
	JSONOmitEmpty bool `json:"jsonOmitEmpty"`

	// JSONOmit is go.json_tags.omit.
	JSONOmit bool `json:"jsonOmit"`

	// EmptySlices is go.empty_slices.
	EmptySlices bool `json:"emptySlices"`

	// DocComments is go.doc_comments.
	DocComments bool `json:"docComments"`

	// ReuseModels is go.reuse_models.
	ReuseModels bool `json:"reuseModels"`

	// WrapErrors is go.wrap_errors.
	WrapErrors bool `json:"wrapErrors"`
}

// StageView is the time one stage of the generation took.
type StageView struct {
	// Stage names the stage, such as generate.catalogue, or vet and catalogue for the passes
	// after generation.
	Stage string `json:"stage"`

	// MS is the stage's total time in milliseconds.
	MS float64 `json:"ms"`
}

// FileView is one generated file.
type FileView struct {
	// Name is the file name, such as models.go.
	Name string `json:"name"`

	// Content is the file's Go source, header included.
	Content string `json:"content"`
}

// Counts counts diagnostics by severity.
type Counts struct {
	// Errors counts error-severity diagnostics, including those of skipped queries in
	// partial output.
	Errors int `json:"errors"`

	// Warnings counts the warnings.
	Warnings int `json:"warnings"`

	// Hints counts the hints.
	Hints int `json:"hints"`
}

// QueryView describes one analysed query and the Go generated for it.
type QueryView struct {
	// Name is the query's name.
	Name string `json:"name"`

	// Command is the query's command, such as one or many.
	Command string `json:"command"`

	// File is the query file's path.
	File string `json:"file"`

	// SQL is the query block as written, header comments included.
	SQL string `json:"sql"`

	// Description is the prose of the query's header, the doc comment go.doc_comments gives
	// its method, or empty when the header holds none.
	Description string `json:"description"`

	// RowType names the struct each row is read into: the query's own row type, a table's
	// model or a row type queries share; it is empty when the query returns no rows or its
	// method was not generated.
	RowType string `json:"rowType"`

	// RowKind says which RowType is: "own" for the query's own row type, "model" for a
	// table's model, as a model: keyword or go.reuse_models chooses, and "shared" for a row
	// type a row: keyword names; it is empty when RowType is.
	RowKind string `json:"rowKind"`

	// SQLSent is the SQL the generated method sends.
	SQLSent string `json:"sqlSent"`

	// SQLSentFrom is "generated" when SQLSent is read from the generated code, or "as
	// written" when no generated code was found and SQLSent repeats SQL.
	SQLSentFrom string `json:"sqlSentFrom"`

	// SQLSentConst names the generated constant SQLSent is read from, or is empty.
	SQLSentConst string `json:"sqlSentConst"`

	// CountSQL is the count query a runtime-built query's Count sends before its filters are
	// added, as the generated code holds it, or empty.
	CountSQL string `json:"countSQL"`

	// GoFile is the generated file holding the method, or empty.
	GoFile string `json:"goFile"`

	// Signature is the generated method's signature, or empty.
	Signature string `json:"signature"`

	// Types holds the declarations of the types the generated method accepts and returns.
	Types string `json:"types"`

	// Params describes the query's parameters.
	Params []ParamView `json:"params"`

	// Columns describes the query's result columns.
	Columns []ResultColumnView `json:"columns"`

	// AllowedColumns lists the columns a dynamic query may filter and order by.
	AllowedColumns []string `json:"allowedColumns"`

	// Sortable lists the columns a sgl.sortable parameter may order by.
	Sortable []string `json:"sortable"`

	// GroupBy lists the columns that collapse repeated parent rows.
	GroupBy []string `json:"groupBy"`

	// Line is the query's one-based line in its file.
	Line int `json:"line"`

	// GoLine is the method's one-based line in GoFile, or zero.
	GoLine int `json:"goLine"`

	// ReadOnly reports whether the query only reads.
	ReadOnly bool `json:"readOnly"`

	// Dynamic reports whether the query generates a runtime builder.
	Dynamic bool `json:"dynamic"`

	// Optional reports whether a missing row is returned as false rather than an error.
	Optional bool `json:"optional"`

	// Skipped reports that a run skipping failing queries left the query out of Files
	// because of an error of its own, which Diagnostics holds.
	Skipped bool `json:"skipped"`
}

// ParamView describes one query parameter.
type ParamView struct {
	// Default is the value used when the caller passes zero, or nil.
	Default *int `json:"default"`

	// MaxLimit is the value larger limits are clamped to, or nil when there is no cap.
	MaxLimit *int `json:"maxLimit"`

	// Name is the parameter's name.
	Name string `json:"name"`

	// SQLType is the parameter's SQL type.
	SQLType string `json:"sqlType"`

	// GoType is the parameter's Go type in the generated code, or empty.
	GoType string `json:"goType"`

	// Number is the parameter's one-based position.
	Number int `json:"number"`

	// Nullable reports whether the parameter accepts NULL.
	Nullable bool `json:"nullable"`

	// Slice reports whether the parameter expands to a list.
	Slice bool `json:"slice"`

	// Optional reports whether the parameter may be left out.
	Optional bool `json:"optional"`
}

// ResultColumnView describes one result column of a query.
type ResultColumnView struct {
	// Name is the column's name.
	Name string `json:"name"`

	// SQLType is the column's SQL type.
	SQLType string `json:"sqlType"`

	// Category is the SQL type's category, such as integer or text.
	Category string `json:"category"`

	// GoType is the column's Go type in the generated row, or empty.
	GoType string `json:"goType"`

	// Source is the table column it reads, as table.column, or empty.
	Source string `json:"source"`

	// Embedded names the sgl.embed group the column belongs to, its as: name or else its
	// table, or is empty for a column that is not embedded.
	Embedded string `json:"embedded"`

	// EmbedTable names the table or view of the column's sgl.embed group, or is empty.
	EmbedTable string `json:"embedTable"`

	// Nullable reports whether the column may be NULL.
	Nullable bool `json:"nullable"`

	// Outer reports whether the embed group comes from an outer join.
	Outer bool `json:"outer"`
}

// respond builds the response of a completed run.
//
// Takes files (map[string]string) which maps request paths to contents.
// Takes resolved (*project) which is the configuration used.
// Takes outcome (*passes) which holds the outcome of each pass.
// Takes stages ([]StageView) which holds the stage timings.
//
// Returns Response which describes the run.
func respond(files map[string]string, resolved *project, outcome *passes, stages []StageView) Response {
	response := emptyResponse
	response.OK = true
	response.Config = resolved.view()
	response.Stages = stages
	response.Files = fileViews(outcome.result.Files)
	response.Diagnostics = diagnosticViews(files, resolved, outcome.result.Diagnostics)
	response.Counts = countDiagnostics(outcome.result.Diagnostics)
	response.Partial = outcome.result.Partial
	index := newGoIndex(outcome.result.Files, resolved.jsonTags())
	response.Queries = make([]QueryView, 0, len(outcome.report.Queries))
	for _, query := range outcome.report.Queries {
		view := queryView(files, resolved, query, index)
		view.Skipped = response.Partial && view.Signature == ""
		response.Queries = append(response.Queries, view)
	}
	response.Catalogue = catalogueView(files, resolved, outcome.catalogue)
	return response
}

// fileViews converts the generated files, sorted by name.
//
// Takes generated ([]emit.GeneratedFile) which are the generated files.
//
// Returns []FileView which holds one entry per file.
func fileViews(generated []emit.GeneratedFile) []FileView {
	views := make([]FileView, 0, len(generated))
	for _, file := range generated {
		views = append(views, FileView{Name: file.Name, Content: string(file.Content)})
	}
	slices.SortFunc(views, func(left, right FileView) int {
		return strings.Compare(left.Name, right.Name)
	})
	return views
}

// countDiagnostics counts diagnostics by severity.
//
// Takes diagnostics ([]seagull.SourceError) which are counted.
//
// Returns Counts which holds the counts.
func countDiagnostics(diagnostics []seagull.SourceError) Counts {
	counts := Counts{Errors: 0, Warnings: 0, Hints: 0}
	for index := range diagnostics {
		switch diagnostics[index].Severity {
		case seagull.SeverityError:
			counts.Errors++
		case seagull.SeverityWarning:
			counts.Warnings++
		default:
			counts.Hints++
		}
	}
	return counts
}

// queryView describes one analysed query and its generated Go.
//
// Takes files (map[string]string) which maps request paths to contents.
// Takes resolved (*project) which maps file names back to request paths.
// Takes query (*emit.AnalysedQuery) which is described.
// Takes index (*goIndex) which holds the parsed generated code.
//
// Returns QueryView which describes the query.
func queryView(files map[string]string, resolved *project, query *emit.AnalysedQuery, index *goIndex) QueryView {
	written := strings.TrimSpace(query.SQL)
	view := QueryView{
		Name:           query.Name,
		Command:        nameOf(commandNames, query.Command),
		File:           resolved.queryPath(files, query.Filename),
		SQL:            written,
		Description:    query.Description,
		RowType:        "",
		RowKind:        "",
		SQLSent:        written,
		SQLSentFrom:    sentAsWritten,
		SQLSentConst:   "",
		CountSQL:       "",
		GoFile:         "",
		Signature:      "",
		Types:          "",
		Params:         nil,
		Columns:        nil,
		AllowedColumns: allowedColumnNames(query.AllowedColumns),
		Sortable:       sortableColumns(query),
		GroupBy:        nonNil(slices.Clone(query.GroupByKey)),
		Line:           query.Line,
		GoLine:         0,
		ReadOnly:       query.ReadOnly,
		Dynamic:        query.DynamicRuntime,
		Optional:       query.Optional,
		Skipped:        false,
	}
	generated := index.describe(query)
	view.RowType, view.RowKind = generated.rowType, rowKind(query, generated.rowType)
	view.GoFile, view.GoLine = generated.file, generated.line
	view.CountSQL = generated.countSQL
	view.Signature, view.Types = generated.signature, generated.types
	if generated.constant != "" {
		view.SQLSent, view.SQLSentFrom, view.SQLSentConst = generated.sql, sentFromGenerated, generated.constant
	}
	view.Params = paramViews(query.Parameters, generated.parameterTypes)
	view.Columns = columnViews(query.OutputColumns, generated.columnTypes)
	return view
}

// rowKind says which struct a query's rows are read into: its own row type, a row type a
// row: keyword names, or otherwise a table's model.
//
// Takes query (*emit.AnalysedQuery) which is described.
// Takes rowType (string) which names the struct, or is empty.
//
// Returns string which is rowKindOwn, rowKindShared or rowKindModel, or empty when
// rowType is.
func rowKind(query *emit.AnalysedQuery, rowType string) string {
	switch {
	case rowType == "":
		return ""
	case rowType == query.Name+rowSuffix:
		return rowKindOwn
	case query.RowType != "":
		return rowKindShared
	default:
		return rowKindModel
	}
}

// paramViews describes a query's parameters.
//
// Takes parameters ([]emit.QueryParameter) which are described.
// Takes goTypes ([]string) which holds the Go type of each parameter by position; it may
// be shorter than parameters.
//
// Returns []ParamView which holds one entry per parameter.
func paramViews(parameters []emit.QueryParameter, goTypes []string) []ParamView {
	views := make([]ParamView, 0, len(parameters))
	for index := range parameters {
		parameter := &parameters[index]
		views = append(views, ParamView{
			Default:  cloneInt(parameter.DefaultLimit),
			MaxLimit: cloneInt(parameter.MaxLimit),
			Name:     parameter.Name,
			SQLType:  assemble.SQLTypeName(parameter.SQLType),
			GoType:   at(goTypes, index),
			Number:   parameter.Number,
			Nullable: parameter.Nullable,
			Slice:    parameter.IsSlice,
			Optional: parameter.IsOptional,
		})
	}
	return views
}

// columnViews describes a query's result columns.
//
// Takes columns ([]emit.OutputColumn) which are described.
// Takes goTypes ([]string) which holds the Go type of each column by position; it may be
// shorter than columns.
//
// Returns []ResultColumnView which holds one entry per column.
func columnViews(columns []emit.OutputColumn, goTypes []string) []ResultColumnView {
	views := make([]ResultColumnView, 0, len(columns))
	for index := range columns {
		column := &columns[index]
		source := ""
		if column.SourceTable != "" && column.SourceColumn != "" {
			source = column.SourceTable + "." + column.SourceColumn
		}
		embedded, table := "", ""
		if column.IsEmbedded {
			embedded, table = embedName(column), column.EmbedTable
		}
		views = append(views, ResultColumnView{
			Name:       column.Name,
			SQLType:    assemble.SQLTypeName(column.SQLType),
			Category:   nameOf(categoryNames, column.SQLType.Category),
			GoType:     at(goTypes, index),
			Source:     source,
			Embedded:   embedded,
			EmbedTable: table,
			Nullable:   column.Nullable,
			Outer:      column.IsEmbedded && column.EmbedIsOuter,
		})
	}
	return views
}

// embedName names the sgl.embed group a column belongs to: its as: name, or its table
// when the model leaves the name empty, as the emitter names it.
//
// Takes column (*emit.OutputColumn) which is an embedded column.
//
// Returns string which is the group's name.
func embedName(column *emit.OutputColumn) string {
	if column.EmbedName != "" {
		return column.EmbedName
	}
	return column.EmbedTable
}

// allowedColumnNames lists the names of a dynamic query's allowed columns.
//
// Takes allowed ([]emit.AllowedColumn) which are listed.
//
// Returns []string which holds the names in order.
func allowedColumnNames(allowed []emit.AllowedColumn) []string {
	names := make([]string, 0, len(allowed))
	for index := range allowed {
		names = append(names, allowed[index].Name)
	}
	return names
}

// sortableColumns lists the columns a query's sgl.sortable directives allow, each once.
//
// Takes query (*emit.AnalysedQuery) which holds the parameters the directives declare.
//
// Returns []string which holds the columns in the order they are declared.
func sortableColumns(query *emit.AnalysedQuery) []string {
	var columns []string
	for index := range query.Parameters {
		columns = append(columns, query.Parameters[index].SortableColumns...)
	}
	unique := make([]string, 0, len(columns))
	for _, column := range columns {
		if !slices.Contains(unique, column) {
			unique = append(unique, column)
		}
	}
	return unique
}

// nameOf looks a value up in a name table.
//
// Takes names (map[K]string) which is the table.
// Takes value (K) which is looked up.
//
// Returns string which is the name, or "unknown" when the table lacks the value.
func nameOf[K comparable](names map[K]string, value K) string {
	if name, found := names[value]; found {
		return name
	}
	return unknownName
}

// at returns one element of a slice, or empty when the slice is too short.
//
// Takes values ([]string) which is indexed.
// Takes index (int) which is the position.
//
// Returns string which is the element, or empty.
func at(values []string, index int) string {
	if index < len(values) {
		return values[index]
	}
	return ""
}

// cloneInt copies an optional integer, so the response shares nothing with the model.
//
// Takes value (*int) which is copied.
//
// Returns *int which is a fresh copy, or nil.
func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	return new(*value)
}

// nonNil returns values, or an empty slice in place of nil, so it encodes as [].
//
// Takes values ([]string) which is checked.
//
// Returns []string which is never nil.
func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
