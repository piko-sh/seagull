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
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const fakeDatabasePath = "example.com/fakedb"

const fakeDatabaseSource = `package fakedb

import "context"

type Tx struct{}

func (*Tx) Exec(context.Context, string, ...any) error { return nil }

func (*Tx) Commit(context.Context) error { return nil }

func (*Tx) Rollback(context.Context) error { return nil }

type Pool struct{}

func (*Pool) Exec(context.Context, string, ...any) error { return nil }

func (*Pool) Begin(context.Context) (*Tx, error) { return nil, nil }
`

type sourceImporter struct {
	packages map[string]*types.Package
	fallback types.Importer
}

func (i sourceImporter) Import(path string) (*types.Package, error) {
	if known, found := i.packages[path]; found {
		return known, nil
	}
	return i.fallback.Import(path)
}

var statementParameters = []emitkit.DBTXParameter{
	{Name: "ctx", Type: goType("context", "Context"), Variadic: false},
	{Name: "query", Type: goType("", "string"), Variadic: false},
	{Name: "args", Type: goType("", "any"), Variadic: true},
}

var databaseSQLSpec = emitkit.QuerierSpec{
	Transactions: &emitkit.TransactionSpec{
		Type:            goType("database/sql", "*Tx"),
		Parameter:       "transaction",
		Begin:           "BeginTx",
		Source:          emitkit.DBTXParameter{Name: "db", Type: goType("database/sql", "*DB"), Variadic: false},
		BeginOptions:    true,
		EndTakesContext: false,
	},
	Connections: emitkit.Connections{Reader: "reader", Writer: "writer"},
	DBTX: []emitkit.DBTXMethod{
		{Name: "ExecContext", Parameters: statementParameters, Results: []sqlmodel.GoType{goType("database/sql", "Result"), goType("", "error")}},
		{Name: "QueryContext", Parameters: statementParameters, Results: []sqlmodel.GoType{goType("database/sql", "*Rows"), goType("", "error")}},
	},
	Replica:  true,
	WrapDBTX: true,
}

var contextEndingSpec = emitkit.QuerierSpec{
	Transactions: &emitkit.TransactionSpec{
		Type:            goType(fakeDatabasePath, "*Tx"),
		Parameter:       "transaction",
		Begin:           "Begin",
		Source:          emitkit.DBTXParameter{Name: "pool", Type: goType(fakeDatabasePath, "*Pool"), Variadic: false},
		BeginOptions:    false,
		EndTakesContext: true,
	},
	Connections: emitkit.Connections{Reader: "db", Writer: "db"},
	DBTX: []emitkit.DBTXMethod{
		{Name: "Exec", Parameters: statementParameters, Results: []sqlmodel.GoType{goType("", "error")}},
	},
	Replica:  false,
	WrapDBTX: false,
}

func newSourceImporter(t *testing.T) types.Importer {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "fakedb.go", fakeDatabaseSource, 0)
	require.NoError(t, err)
	fallback := importer.Default()
	config := types.Config{Importer: fallback}
	checked, err := config.Check(fakeDatabasePath, fileSet, []*ast.File{file}, nil)
	require.NoError(t, err)
	return sourceImporter{packages: map[string]*types.Package{fakeDatabasePath: checked}, fallback: fallback}
}

func goType(packagePath, name string) sqlmodel.GoType {
	return sqlmodel.GoType{Composite: nil, Package: packagePath, Name: name}
}

func TestQuerierGeneratesAPackageThatTypeChecks(t *testing.T) {
	t.Parallel()

	withoutTransactions := contextEndingSpec
	withoutTransactions.Transactions = nil
	documented := databaseSQLSpec
	documented.PackageNote = "needs a setting on its connection."
	testCases := []struct {
		name    string
		spec    emitkit.QuerierSpec
		want    []string
		unwants []string
	}{
		{
			name: "replica, wrapper and options-taking transactions",
			spec: databaseSQLSpec,
			want: []string{
				"func NewWithReplica(",
				"type dbtxWrapper interface",
				"func (queries *Queries) WithTx(transaction *sql.Tx) *Queries",
				"func (queries *Queries) RunInTx(ctx context.Context, db *sql.DB, fn func(*Queries) error) error",
				"db.BeginTx(ctx, nil)",
			},
			unwants: []string{"Rollback(ctx)", "Commit(ctx)"},
		},
		{
			name: "transactions that end with the context",
			spec: contextEndingSpec,
			want: []string{
				"func (queries *Queries) RunInTx(ctx context.Context, pool *fakedb.Pool, fn func(*Queries) error) error",
				"pool.Begin(ctx)",
				"Commit(ctx)",
			},
			unwants: []string{"NewWithReplica", "dbtxWrapper"},
		},
		{
			name:    "one connection without transactions",
			spec:    withoutTransactions,
			want:    []string{"type DBTX interface", "func New("},
			unwants: []string{"WithTx", "RunInTx", "fakedb", "// Package"},
		},
		{
			name:    "a package note",
			spec:    documented,
			want:    []string{"// Package store needs a setting on its connection.\npackage store\n"},
			unwants: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := emitkit.Querier("store", emit.Naming{}, testCase.spec)
			require.NoError(t, err)

			require.Equal(t, "querier.go", file.Name)
			source := string(file.Content)
			for _, want := range testCase.want {
				require.Contains(t, source, want)
			}
			for _, unwant := range testCase.unwants {
				require.NotContains(t, source, unwant)
			}
			fileSet := token.NewFileSet()
			syntax, err := parser.ParseFile(fileSet, file.Name, file.Content, 0)
			require.NoError(t, err)
			config := types.Config{Importer: newSourceImporter(t)}
			_, err = config.Check("store", fileSet, []*ast.File{syntax}, nil)
			require.NoErrorf(t, err, "%s", source)
		})
	}
}

func TestQuerierRejectsAReplicaSharingOneConnection(t *testing.T) {
	t.Parallel()

	spec := contextEndingSpec
	spec.Replica = true

	file, err := emitkit.Querier("store", emit.Naming{}, spec)

	require.ErrorContains(t, err, "a replica needs separate reader and writer fields")
	require.Empty(t, file.Name)
	require.Nil(t, file.Content)
}

func TestQuerierDeclaresTheTransactionHelpersOfASpec(t *testing.T) {
	t.Parallel()

	withoutTransactions := contextEndingSpec
	withoutTransactions.Transactions = nil
	testCases := []struct {
		name  string
		spec  emitkit.QuerierSpec
		names []string
	}{
		{name: "wrapping transactions", spec: databaseSQLSpec, names: []string{"WithTx", "wrapDBTX", "RunInTx"}},
		{name: "plain transactions", spec: contextEndingSpec, names: []string{"WithTx", "RunInTx"}},
		{name: "no transactions", spec: withoutTransactions, names: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := emitkit.Querier("store", emit.Naming{}, testCase.spec)
			require.NoError(t, err)
			syntax, err := parser.ParseFile(token.NewFileSet(), file.Name, file.Content, 0)
			require.NoError(t, err)

			var names []string
			for _, declaration := range syntax.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if isFunction && function.Recv != nil && slices.Contains([]string{"WithTx", "wrapDBTX", "RunInTx"}, function.Name.Name) {
					names = append(names, function.Name.Name)
				}
			}

			require.Equal(t, testCase.names, names)
		})
	}
}
