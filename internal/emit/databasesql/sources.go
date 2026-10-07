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

package databasesql

import (
	"cmp"
	"embed"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// preparedFile is the generated file holding the prepared-statement cache.
	preparedFile = "prepared.go"

	// duckDBModule is the import path of the DuckDB driver, whose map type the
	// driver-composite conversion binds a MAP as.
	duckDBModule = "github.com/duckdb/duckdb-go/v2"
)

// sources holds the sources of the helpers the emitter adds to a generated package.
//
//go:embed internal/source/driver_values.go internal/source/driver_composites.go
//go:embed internal/source/prepared.go
var sources embed.FS

// preparedSources caches immutable parsed helper templates for this emitter.
var preparedSources = helpers.NewSources(sources, "internal/source")

// driverWrappers says which parameter wrappers driver_values.go holds.
type driverWrappers struct {
	// values includes the wrapper binding a parameter through its driver.Valuer.
	values bool

	// text includes the wrapper binding bytes as text.
	text bool

	// documents includes the wrapper binding a JSON parameter as the document it encodes to.
	documents bool

	// binary includes the wrapper binding a binary parameter as the bytes it marshals itself
	// to.
	binary bool

	// decimals includes the wrapper binding a decimal held by a pointer that database/sql
	// would hand the driver as it is.
	decimals bool
}

// driverComposites says which parts of driver_composites.go a package needs.
type driverComposites struct {
	// binds includes the conversion of parameters into the forms the driver binds.
	binds bool

	// lists includes the list-inference wrapper, which hands the driver a list it binds by
	// the types of its elements; it needs binds too.
	lists bool

	// scans includes the scan adapter reading the forms the driver returns a LIST, STRUCT or
	// MAP column in.
	scans bool

	// overrides includes the scan adapter reading a column whose Go type an override chose.
	overrides bool

	// documentOverrides includes the scan adapter decoding a JSON column's document into the
	// Go type an override chose.
	documentOverrides bool

	// ownTypes includes the reading of the types of its own the driver returns a DECIMAL and
	// an INTERVAL in.
	ownTypes bool

	// documents includes the reading of the JSON documents the driver returns as its own,
	// through clickhouse_documents.go.
	documents bool
}

// renderSource renders one of the emitter's helper sources into a generated file of the
// same name.
//
// Takes name (string) which is the source's file name, which the generated file keeps.
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes data (*helpers.Data) which holds the flags, values and package name.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be read or rendered.
func renderSource(name string, vocabulary helpers.Vocabulary, data *helpers.Data) (sqlmodel.GeneratedFile, error) {
	return preparedSources.File(name, name, vocabulary, data)
}

// renderDriverValues renders driver_values.go: the wrapper that hands database/sql a
// parameter through its driver.Valuer, the one that binds a parameter's bytes as text,
// and the others the package uses.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
// Takes wrappers (driverWrappers) which says which wrappers the file holds.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func renderDriverValues(vocabulary helpers.Vocabulary, packageName string, wrappers driverWrappers) (sqlmodel.GeneratedFile, error) {
	return renderSource(driverValueHelperFile, vocabulary, &helpers.Data{
		Flags: map[string]bool{
			"value": wrappers.values, "text": wrappers.text, "json": wrappers.documents, "binary": wrappers.binary,
			"decimal": wrappers.decimals,
		},
		Values:  nil,
		Renames: nil,
		Package: packageName,
	})
}

// renderDriverComposites renders the parameter conversion and column scan helpers a
// package needs.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
// Takes parts (driverComposites) which says which parts the package uses.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func renderDriverComposites(vocabulary helpers.Vocabulary, packageName string, parts driverComposites) (sqlmodel.GeneratedFile, error) {
	reads := parts.scans || parts.overrides || parts.documentOverrides
	return renderSource(driverCompositeHelperFile, vocabulary, &helpers.Data{
		Flags: map[string]bool{
			"bind": parts.binds, "lists": parts.lists, "scan": parts.scans, "override": parts.overrides, "overrideJSON": parts.documentOverrides,
			"reads": reads, "ownTypes": parts.ownTypes, "standIns": false,
		},
		Values: map[string]any{
			"mapType":        helpers.Code{Text: "duckdb.Map", Imports: []string{duckDBModule}},
			"intervalType":   helpers.Code{Text: "duckdb.Interval", Imports: []string{duckDBModule}},
			"decimalType":    helpers.Code{Text: "duckdb.Decimal", Imports: []string{duckDBModule}},
			"readsOwnTypes":  parts.ownTypes,
			"readsDocuments": parts.documents,
		},
		Renames: nil,
		Package: packageName,
	})
}

// renderPrepared renders prepared.go: the PreparedDBTX that prepares every static query
// once and reuses the statements, inside transactions too.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated package's identifiers.
// Takes request (*emit.Request) which holds the package's name, queries and naming plan;
// the static queries are prepared.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func renderPrepared(vocabulary helpers.Vocabulary, request *emit.Request) (sqlmodel.GeneratedFile, error) {
	return renderSource(preparedFile, vocabulary, &helpers.Data{
		Flags:   nil,
		Values:  map[string]any{"staticQueries": helpers.Code{Text: "[]string{" + strings.Join(staticConstants(request), ", ") + "}", Imports: nil}},
		Renames: nil,
		Package: request.Package,
	})
}

// staticConstants returns static-query SQL constants in naming-plan order. It creates a
// plan when the request has none.
//
// Takes request (*emit.Request) which holds the queries and the naming plan.
//
// Returns []string which holds the constants.
func staticConstants(request *emit.Request) []string {
	naming := request.Naming
	if len(naming.Queries) != len(request.Queries) {
		var style emitkit.NameStyle
		style.SQLConstantSuffix = request.Style.SQLConstantSuffix
		naming, _ = emitkit.PlanPackage(request, emitkit.DefaultReservation(&request.Dialect, &request.Style), style)
	}
	order := make([]int, 0, len(request.Queries))
	for index, query := range request.Queries {
		if isStaticQuery(query) && index < len(naming.Queries) {
			order = append(order, index)
		}
	}
	slices.SortStableFunc(order, func(first, second int) int {
		return cmp.Or(
			cmp.Compare(request.Queries[first].Filename, request.Queries[second].Filename),
			cmp.Compare(request.Queries[first].Name, request.Queries[second].Name),
		)
	})
	constants := make([]string, len(order))
	for position, index := range order {
		constants[position] = naming.Queries[index].SQLConstant
	}
	return constants
}

// isStaticQuery reports whether a query has a fixed SQL string that does not change at
// runtime. Dynamic queries (sortable ORDER BY, runtime builder) produce different SQL
// strings, which run unprepared, and batch and copyfrom queries expand their VALUES tuple
// per call, so their SQL constant never runs as written.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query to check.
//
// Returns bool which is true when the query SQL is fixed.
func isStaticQuery(query *sqlmodel.AnalysedQuery) bool {
	if query.IsDynamic || query.DynamicRuntime {
		return false
	}

	if query.Command == sqlmodel.QueryCommandBatch || query.Command == sqlmodel.QueryCommandCopyFrom {
		return false
	}
	for i := range query.Parameters {
		if query.Parameters[i].IsSlice {
			return false
		}
	}
	return true
}
