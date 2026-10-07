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
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

type reservingEmitter struct{}

func (reservingEmitter) Emit(context.Context, *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	return nil, nil
}

func (reservingEmitter) Reserve(info *dialect.Facts, style *emit.Style) emit.Reservation {
	return emit.Reservation{Receiver: info.Name + style.HelperPrefix}
}

type plainEmitter struct{}

func (plainEmitter) Emit(context.Context, *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	return nil, nil
}

var helperSourceDirectories = []string{
	"../../gogen/helpers/source",
	"../databasesql/internal/source",
	"../internal/chvalues/source",
	"../internal/valuereading/source",
}

func TestDefaultReservationKeepsTheReceiverApartFromTheLocals(t *testing.T) {
	t.Parallel()

	reservation := defaultReservation()

	require.Equal(t, "queries", reservation.Receiver)
	require.NotContains(t, reservation.Locals, reservation.Receiver)
	require.IsIncreasing(t, reservation.Locals)
}

func TestDefaultReservationNamesHelpersAfterTheStyle(t *testing.T) {
	t.Parallel()

	reservation := emitkit.DefaultReservation(&dialect.Facts{}, &emit.Style{HelperPrefix: "acme", ErrorPrefix: "acme"})

	for _, declaration := range []string{
		"Queries", "New", "NewWithReplica", "DBTX", "dbtxWrapper", "Prepare", "PreparedDBTX", "preparedTx",
		"OrderDirection", "OrderAsc", "OrderDesc", "queryNameMap",
		"acmeMaxBindVariables", "acmeSplice", "acmeList", "acmeScanInto", "acmeJSONScanner",
		"acmeClickHouseValue", "acmeDriverComposite", "acmeNewPredicate",
		"errAcmeClickHouseValue", "ErrAcmeTooManyBindVariables", "ErrAcmeUnknownColumn",
		"ErrAcmeUnknownDirection", "ErrAcmeUnknownOperator",
	} {
		require.Contains(t, reservation.Declarations, declaration)
	}
	require.NotContains(t, reservation.Declarations, emit.InlineJSONTypeName)
	require.NotContains(t, reservation.Declarations, "errAcmeTooManyBindVariables")
	for _, file := range []string{"querier.go", "models.go", "prepared.go", "sortable.go", "bind_limits.go", "clickhouse_values.go", "acme_json.go"} {
		require.Contains(t, reservation.Files, file)
	}
	require.Subset(t, reservation.Methods, []string{"WithTx", "RunInTx", "wrapDBTX", "reader", "writer"})
}

func TestDefaultReservationKeepsTheDriverQualifierForDatabaseSQL(t *testing.T) {
	t.Parallel()

	naming, problems := emitkit.PlanPackage(requestOf(nil), defaultReservation(), emitkit.NameStyle{})

	require.Empty(t, problems)
	require.Equal(t, "driver", naming.Imports["database/sql/driver"])
	require.Equal(t, "driver2", naming.Imports["github.com/ClickHouse/clickhouse-go/v2/lib/driver"])
	require.Equal(t, "sql", naming.Imports["database/sql"])
	require.Equal(t, "decimal", naming.Imports["github.com/shopspring/decimal"])
	require.Equal(t, "apd", naming.Imports["github.com/cockroachdb/apd/v3"])
	require.Equal(t, "duckdb", naming.Imports["github.com/duckdb/duckdb-go/v2"])
}

func TestReservationForPrefersTheEmittersOwn(t *testing.T) {
	t.Parallel()

	info := &dialect.Facts{Name: "martian"}
	style := sglStyle()

	require.Equal(t, emit.Reservation{Receiver: "martiansgl"}, emitkit.ReservationFor(reservingEmitter{}, info, style))
	require.Equal(t, emitkit.DefaultReservation(info, style), emitkit.ReservationFor(plainEmitter{}, info, style))
}

func helperSourceDeclarations(t *testing.T) []string {
	t.Helper()

	var names []string
	for _, directory := range helperSourceDirectories {
		files, err := filepath.Glob(filepath.Join(directory, "*.go"))
		require.NoError(t, err)
		require.NotEmpty(t, files, directory)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") || filepath.Base(file) == "descriptor.go" || filepath.Base(file) == "exports.go" {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ParseComments)
			require.NoError(t, err)
			for _, declaration := range parsed.Decls {
				names = append(names, renderedNames(declaration)...)
			}
		}
	}
	return names
}

func renderedNames(declaration ast.Decl) []string {
	switch typed := declaration.(type) {
	case *ast.FuncDecl:
		if typed.Recv != nil || isInlined(typed.Doc) {
			return nil
		}
		return []string{typed.Name.Name}
	case *ast.GenDecl:
		if isInlined(typed.Doc) {
			return nil
		}
		var names []string
		for _, spec := range typed.Specs {
			switch named := spec.(type) {
			case *ast.TypeSpec:
				names = append(names, named.Name.Name)
			case *ast.ValueSpec:
				for _, name := range named.Names {
					names = append(names, name.Name)
				}
			}
		}
		return names
	}
	return nil
}

func isInlined(doc *ast.CommentGroup) bool {
	return doc != nil && slices.ContainsFunc(doc.List, func(comment *ast.Comment) bool {
		return strings.HasPrefix(comment.Text, "//sgl:inline ")
	})
}

func styledHelperName(canonical string) (string, bool) {
	for _, rename := range [][2]string{{"ErrSgl", "ErrAcmeStore"}, {"errSgl", "errAcme"}, {"sgl", "acme"}} {
		if rest, found := strings.CutPrefix(canonical, rename[0]); found {
			return rename[1] + rest, true
		}
	}
	return canonical, false
}

func acmeStyle() *emit.Style {
	return &emit.Style{HelperPrefix: "acme", ErrorPrefix: "acme_store", QueryNames: emit.QueryNames{Function: "QueryName"}}
}

func TestDefaultReservationReservesEveryDeclarationOfTheHelperSources(t *testing.T) {
	t.Parallel()

	style := acmeStyle()
	reservation := emitkit.DefaultReservation(&dialect.Facts{}, style)

	for _, canonical := range helperSourceDeclarations(t) {
		if canonical == emit.InlineJSONTypeName || canonical == style.QueryNames.Function {
			continue
		}
		name, _ := styledHelperName(canonical)
		require.Contains(t, reservation.Declarations, name, "the helper sources declare %s", canonical)
	}
}

func TestDefaultReservationReservesNoHelperTheSourcesDoNotDeclare(t *testing.T) {
	t.Parallel()

	declared := make(map[string]bool)
	for _, canonical := range helperSourceDeclarations(t) {
		if name, prefixed := styledHelperName(canonical); prefixed {
			declared[name] = true
		}
	}
	reservation := emitkit.DefaultReservation(&dialect.Facts{}, acmeStyle())

	for _, name := range reservation.Declarations {
		if strings.HasPrefix(name, "acme") || strings.HasPrefix(name, "errAcme") || strings.HasPrefix(name, "ErrAcmeStore") {
			require.True(t, declared[name], "%s is reserved but no helper source declares it", name)
		}
	}
	require.NotContains(t, reservation.Files, "batch_helpers.go")
}

func TestDefaultReservationPlansTheIterImportOnlyForCurrentIdioms(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		modern bool
	}{
		{name: "current idioms", modern: true},
		{name: "the earlier layout", modern: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			style := sglStyle()
			style.ModernIdioms = testCase.modern
			reservation := emitkit.DefaultReservation(&dialect.Facts{}, style)
			naming, problems := emitkit.PlanPackage(requestOf(nil), reservation, emitkit.NameStyle{})

			require.Empty(t, problems)
			require.Equal(t, testCase.modern, slices.Contains(reservation.Imports, "iter"))
			qualifier, planned := naming.Imports["iter"]
			require.Equal(t, testCase.modern, planned)
			if testCase.modern {
				require.Equal(t, "iter", qualifier)
			}
		})
	}
}
