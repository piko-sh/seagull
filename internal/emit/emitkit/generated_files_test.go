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

package emitkit_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
)

const docsDirectory = "../../../docs"

const (
	queryFile          = "<name>.sql.go"
	queryNamesFile     = "query_names.go"
	helperTableStart   = "<!-- generated-file-table: helpers (make generate-all) -->"
	overviewTableStart = "<!-- generated-file-table: overview (make generate-all) -->"
	tableEnd           = "<!-- end generated-file-table -->"
	helperRowContents  = "Small runtime helpers, each written only when a query needs it, so generated code " +
		"needs nothing from Seagull. They are real Go sources in the repository, compiled and tested, and " +
		"rendered into the package (see [generated Go](../reference/generated-go.md#helper-files))."
)

var updateGeneratedFiles = flag.Bool("update-generated-files", false,
	"rewrite the generated-file tables in reference/generated-go.md and explanation/architecture.md")

type documentedFile struct {
	name     string
	when     string
	contents string
	overview string
}

var documentedFiles = []documentedFile{
	{name: "querier.go", overview: "`DBTX`, `Queries`, constructors, transaction helpers."},
	{name: "querier_interface.go", overview: "The `Querier` interface listing every query method, " +
		"when `go.interface.enabled` is set."},
	{name: "models.go", overview: "One struct per table, and the named types the plan declares: a string type " +
		"with a constant per value and a `Valid` method for each enum, and a struct type for each DuckDB `STRUCT` " +
		"or ClickHouse named `Tuple` the mapping reads as a struct. PostgreSQL composite types map to `any`."},
	{name: "prepared.go", overview: "The prepared-statement cache, when enabled (`go.prepared: true` in `sgl`, " +
		"on by default in the library's `emit_driver_databasesql.New()`)."},
	{name: queryFile, overview: "The methods for one query file."},
	{name: "sortable.go", when: "A query is sortable",
		contents: "`OrderDirection` with `OrderAsc` and `OrderDesc`, declared once for the package."},
	{name: "bind_limits.go", when: "A query expands a slice or is built at run time",
		contents: "The engine's bind-variable limit, `ErrSglTooManyBindVariables`, and the splicer that edits a " +
			"query's SQL at offsets fixed when the code was generated."},
	{name: "slice_helpers.go", when: "A query expands a slice",
		contents: "The list writer: a placeholder per element, or the dialect's test for an empty list."},
	{name: "runtime_helpers.go", when: "A query is built at run time",
		contents: "The builder's allowed operators, conversions, filters, ordering and pagination, in the " +
			"dialect's syntax, and `ErrSglUnknownColumn`, `ErrSglUnknownOperator` and `ErrSglUnknownDirection`."},
	{name: "sgl_json.go", when: "A JSON value keeps the default JSON slot",
		contents: "The `JSON` type and its scan adapter. The file is named after the helper prefix."},
	{name: "driver_values.go", when: "database/sql, where the driver needs a parameter converted; pgx, where " +
		"a binary column's override type binds its own bytes",
		contents: "Conversions of the parameters a driver does not bind as they are, such as nullable decimals " +
			"and the bytes a binary override marshals itself to."},
	{name: "driver_composites.go", when: "database/sql, where a driver binds or returns lists, structs or " +
		"maps in a form of its own, or a column whose Go type an override chose needs reading through the adapter",
		contents: "The scan adapter reading those forms and the one reading an overridden column (see " +
			"[what an override type must support](type-mapping.md#what-an-override-type-must-support)), and " +
			"the conversion of a list, struct or map parameter into the driver's form, which refuses a value " +
			"nesting more than 32 levels deep or referring to itself."},
	{name: "clickhouse_values.go", when: "A ClickHouse query binds a parameter by name, or a native batch " +
		"writes `clickhouse_appending.go`",
		contents: "The formatter writing each value in the shape its ClickHouse type takes, and the parser of " +
			"those shapes."},
	{name: "clickhouse_appending.go", when: "A native ClickHouse batch or copyfrom appends a value holding an " +
		"IP address, a map of tuples or an overridden Go type",
		contents: "The conversion of each row into the forms clickhouse-go appends, refusing a bad address " +
			"before the batch sends anything. It reads shapes with the parser in `clickhouse_values.go`, so " +
			"that file always comes with it."},
	{name: "clickhouse_composites.go", when: "A native ClickHouse read needs a column converted: an array, map " +
		"or tuple holding IP addresses, wide integers or JSON, a JSON column, or an overridden Go type",
		contents: "The converting scan adapter: the driver fills the form it reads, and the adapter converts it " +
			"into the generated field after the scan."},
	{name: "clickhouse_documents.go", when: "A ClickHouse query reads a JSON value, on either emitter",
		contents: "The conversion of the documents clickhouse-go decodes into the JSON slot's type or an " +
			"override's type, giving the same bytes on both emitters."},
	{name: "value_reading.go", when: "A query reads a column through the scan adapter in " +
		"`driver_composites.go` or `clickhouse_composites.go`",
		contents: "The reflection readers both emitters share: a list into a slice, a map, a Go map or the " +
			"driver's ordered one, into a map, and a scalar into a scalar of its kind, each element through the " +
			"driver's own reader, refusing an integer its target cannot hold. A nil list or map reads as nil."},
	{name: "error_wrapping.go", when: "The preset sets `WrapErrors`",
		contents: "`sglWrapError` and `sglWrapResult`, which label each error a generated method returns with " +
			"the error prefix and the method's name, keeping the error beneath for `errors.Is` and `errors.As` " +
			"(see [labelled errors](#labelled-errors-wraperrors))."},
	{name: "convert_strings.go", when: "A driver binds or reads an enum only as plain strings: pgx, pgx's " +
		"stdlib behind database/sql, the native ClickHouse driver, or ClickHouse behind database/sql",
		contents: "Conversions between an enum's named type and plain strings, in slices, slices of pointers " +
			"and maps."},
	{name: queryNamesFile, when: "The preset enables the query-names file, under the name it gives",
		contents: "The function mapping a statement's SQL to its query's name, for labelling telemetry, " +
			"including SQL a method edits at run time."},
}

func TestTheGeneratedFileTablesListEveryFileTheEmittersWrite(t *testing.T) {
	reservation := emitkit.DefaultReservation(nil, &emit.Style{HelperPrefix: "sgl", ErrorPrefix: "sgl"})

	listed := make([]string, 0, len(documentedFiles))
	for _, file := range documentedFiles {
		listed = append(listed, file.name)
	}
	require.ElementsMatch(t, append([]string{queryFile, queryNamesFile}, reservation.Files...), listed,
		"every file the emitters may write has one entry in documentedFiles, which renders the documented tables")

	pages := []struct {
		path  string
		start string
		table string
	}{
		{path: "reference/generated-go.md", start: helperTableStart, table: helperTable()},
		{path: "explanation/architecture.md", start: overviewTableStart, table: overviewTable()},
	}
	for _, page := range pages {
		path := filepath.Join(docsDirectory, page.path)
		content, err := os.ReadFile(path)
		require.NoError(t, err)

		rendered, err := replaceTable(string(content), page.start, page.table)
		require.NoError(t, err, page.path)

		if *updateGeneratedFiles {
			require.NoError(t, os.WriteFile(path, []byte(rendered), 0o600))
			continue
		}
		require.Equal(t, rendered, string(content),
			"%s holds a stale generated-file table; run make generate-all", page.path)
	}
}

func helperTable() string {
	var table strings.Builder
	table.WriteString("| File | When | Contents |\n|---|---|---|\n")
	for _, file := range documentedFiles {
		if file.overview == "" {
			fmt.Fprintf(&table, "| `%s` | %s | %s |\n", file.name, file.when, file.contents)
		}
	}
	return table.String()
}

func overviewTable() string {
	var table strings.Builder
	var helpers []string
	table.WriteString("| File | Contents |\n|---|---|\n")
	for _, file := range documentedFiles {
		if file.overview == "" {
			helpers = append(helpers, "`"+file.name+"`")
			continue
		}
		fmt.Fprintf(&table, "| `%s` | %s |\n", file.name, file.overview)
	}
	fmt.Fprintf(&table, "| %s | %s |\n", strings.Join(helpers, ", "), helperRowContents)
	return table.String()
}

func replaceTable(content, start, table string) (string, error) {
	before, rest, found := strings.Cut(content, start+"\n")
	if !found {
		return "", fmt.Errorf("no %q marker", start)
	}
	_, after, found := strings.Cut(rest, tableEnd)
	if !found {
		return "", fmt.Errorf("no %q marker after %q", tableEnd, start)
	}
	return before + start + "\n" + table + tableEnd + after, nil
}
