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

package goast

import (
	"go/ast"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestImportTrackerBuiltinType(t *testing.T) {
	tracker := NewImportTracker()
	expression := tracker.AddType(sqlmodel.GoType{Name: "string"})

	require.NotNil(t, expression)
	assert.Empty(t, tracker.imports)
}

func TestImportTrackerExternalPackage(t *testing.T) {
	tracker := NewImportTracker()
	expression := tracker.AddType(sqlmodel.GoType{Package: "time", Name: "Time"})

	require.NotNil(t, expression)
	assert.Contains(t, tracker.imports, "time")
}

func TestImportTrackerPointerType(t *testing.T) {
	tracker := NewImportTracker()
	expression := tracker.AddType(sqlmodel.GoType{Package: "time", Name: "*Time"})

	require.NotNil(t, expression)
	assert.Contains(t, tracker.imports, "time")
}

func TestImportTrackerTypedSliceRegistersElementPackage(t *testing.T) {
	tracker := NewImportTracker()
	expression := tracker.AddType(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "[]UUID"})

	require.NotNil(t, expression)
	assert.Contains(t, tracker.imports, "github.com/google/uuid",
		"AddType must register the element package for a typed-slice array")
}

func TestImportTrackerSliceType(t *testing.T) {
	tracker := NewImportTracker()
	expression := tracker.AddType(sqlmodel.GoType{Name: "[]byte"})

	require.NotNil(t, expression)
	assert.Empty(t, tracker.imports)
}

func TestImportTracker_DistinctPackagesSamePathBaseGetAliased(t *testing.T) {
	t.Parallel()

	tracker := NewImportTracker()
	firstExpression := tracker.AddType(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"})
	secondExpression := tracker.AddType(sqlmodel.GoType{Package: "myapp/uuid", Name: "UUID"})

	firstSelector, firstOk := firstExpression.(*ast.SelectorExpr)
	require.True(t, firstOk, "first AddType should return SelectorExpr")
	firstAlias := firstSelector.X.(*ast.Ident).Name

	secondSelector, secondOk := secondExpression.(*ast.SelectorExpr)
	require.True(t, secondOk, "second AddType should return SelectorExpr")
	secondAlias := secondSelector.X.(*ast.Ident).Name

	assert.NotEqual(t, firstAlias, secondAlias, "colliding path.Base must produce distinct aliases")
	assert.Equal(t, "uuid", firstAlias, "first registrant keeps the natural alias")
	assert.Equal(t, "uuid2", secondAlias, "second registrant gets a numeric-suffixed alias")
}

func TestImportTracker_SamePackageReturnsSameAlias(t *testing.T) {
	t.Parallel()

	tracker := NewImportTracker()
	firstExpression := tracker.AddType(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"})
	secondExpression := tracker.AddType(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "Nil"})

	firstAlias := firstExpression.(*ast.SelectorExpr).X.(*ast.Ident).Name
	secondAlias := secondExpression.(*ast.SelectorExpr).X.(*ast.Ident).Name
	assert.Equal(t, firstAlias, secondAlias, "two types in the same package share one alias")
}

func TestImportTracker_NumericAliasContinuesIncrementing(t *testing.T) {
	t.Parallel()

	tracker := NewImportTracker()
	tracker.AddType(sqlmodel.GoType{Package: "a/uuid", Name: "UUID"})
	tracker.AddType(sqlmodel.GoType{Package: "b/uuid", Name: "UUID"})
	thirdExpression := tracker.AddType(sqlmodel.GoType{Package: "c/uuid", Name: "UUID"})

	thirdAlias := thirdExpression.(*ast.SelectorExpr).X.(*ast.Ident).Name
	assert.Equal(t, "uuid3", thirdAlias, "third collision keeps incrementing")
}

func TestImportQualifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		importPath string
		qualifier  string
		assumed    string
	}{
		{importPath: "time", qualifier: "time", assumed: "time"},
		{importPath: "database/sql", qualifier: "sql", assumed: "sql"},
		{importPath: "github.com/jackc/pgx/v5", qualifier: "pgx", assumed: "pgx"},
		{importPath: "github.com/ClickHouse/clickhouse-go/v2", qualifier: "clickhouse", assumed: "clickhouse"},
		{importPath: "gopkg.in/guregu/null.v4", qualifier: "null", assumed: "null"},
		{importPath: "gopkg.in/yaml.v3", qualifier: "yaml", assumed: "yaml"},
		{importPath: "github.com/Rhymond/go-money", qualifier: "money", assumed: "money"},
		{importPath: "example.com/go-type", qualifier: "type_", assumed: "type"},
		{importPath: "example.com/string", qualifier: "string_", assumed: "string"},
		{importPath: "example.com/2fa", qualifier: "_2fa", assumed: "2fa"},
		{importPath: "example.com/-dash", qualifier: "pkg", assumed: ""},
	}

	for _, test := range tests {
		t.Run(test.importPath, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.qualifier, ImportQualifier(test.importPath))
			require.Equal(t, test.assumed, AssumedPackageName(test.importPath))
		})
	}
}

func TestPlannedImportTrackerWritesThePlannedQualifiers(t *testing.T) {
	t.Parallel()

	tracker := NewPlannedImportTracker(map[string]string{
		"gopkg.in/guregu/null.v4":     "null",
		"github.com/Rhymond/go-money": "money",
		"github.com/google/uuid":      "uuid",
		"example.com/other/uuid":      "uuid2",
		"example.com/go-type":         "type_",
		"github.com/jackc/pgx/v5":     "pgx",
	})
	fields := []sqlmodel.GoField{
		{Name: "Note", Type: sqlmodel.GoType{Package: "gopkg.in/guregu/null.v4", Name: "String"}},
		{Name: "Price", Type: sqlmodel.GoType{Package: "github.com/Rhymond/go-money", Name: "*Money"}},
		{Name: "ID", Type: sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}},
		{Name: "Other", Type: sqlmodel.GoType{Package: "example.com/other/uuid", Name: "UUID"}},
		{Name: "Kind", Type: sqlmodel.GoType{Package: "example.com/go-type", Name: "Kind"}},
		{Name: "Rows", Type: sqlmodel.GoType{Package: "github.com/jackc/pgx/v5", Name: "Rows"}},
		{Name: "Extra", Type: sqlmodel.GoType{Package: "example.com/money", Name: "Coin"}},
	}
	declaration := &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{&ast.TypeSpec{
			Name: ast.NewIdent("Row"),
			Type: tracker.AddType(sqlmodel.NewGoStruct(fields)),
		}},
	}

	content, err := FormatFileWithAST("generated", tracker, []ast.Decl{declaration})
	require.NoError(t, err)
	source := string(content)

	for _, expected := range []string{
		"\"gopkg.in/guregu/null.v4\"",
		"\"github.com/Rhymond/go-money\"",
		"\"github.com/google/uuid\"",
		"uuid2 \"example.com/other/uuid\"",
		"type_ \"example.com/go-type\"",
		"\"github.com/jackc/pgx/v5\"",
		"money2 \"example.com/money\"",
		"null.String",
		"*money.Money",
		"uuid2.UUID",
		"type_.Kind",
		"pgx.Rows",
		"money2.Coin",
	} {
		require.Contains(t, source, expected)
	}
	require.NotContains(t, source, "null \"gopkg.in")
	require.NotContains(t, source, "pgx \"github.com")
}

func TestPlannedImportTrackerKeepsOneQualifierPerPath(t *testing.T) {
	t.Parallel()

	tracker := NewPlannedImportTracker(map[string]string{"github.com/google/uuid": "uuid3"})
	first := tracker.AddType(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"})
	second := tracker.AddType(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "NullUUID"})

	require.Equal(t, "uuid3", first.(*ast.SelectorExpr).X.(*ast.Ident).Name)
	require.Equal(t, "uuid3", second.(*ast.SelectorExpr).X.(*ast.Ident).Name)
	require.Equal(t, map[string]string{"github.com/google/uuid": "uuid3"}, tracker.imports)
}
