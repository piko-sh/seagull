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

package gogen

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/preset"
	"seagull.sh/seagull/internal/sqlmodel"
)

func testRequest() *emit.Request {
	settings := preset.DefaultSettings()
	return &emit.Request{
		Catalogue:         nil,
		Types:             nil,
		Package:           "db",
		Dialect:           dialect.Facts{Name: "test", Family: "test"},
		Queries:           nil,
		DirectivePrefixes: settings.Prefixes.Accepted(),
		Style:             settings.Style,
	}
}

func testVocabulary() Vocabulary {
	return NewVocabulary(testRequest())
}

func TestVocabularyNamesHelpersSentinelsAndMessages(t *testing.T) {
	t.Parallel()

	request := testRequest()
	request.Style.HelperPrefix = "acme"
	request.Style.ErrorPrefix = "acmeerr"
	vocabulary := NewVocabulary(request)

	assert.Equal(t, "acmeMaxBindVariables", vocabulary.Helper("MaxBindVariables"))
	assert.Equal(t, "errAcmeTooManyBindVariables", vocabulary.Sentinel("TooManyBindVariables"))
	assert.Equal(t, "acmeerr: too many bind variables", vocabulary.Message("too many bind variables"))
	assert.Equal(t, "acmeScanInto", vocabulary.InlineScanInto())
}

func TestVocabularyErrorValueIsExportedAndNamedAfterTheErrorPrefix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		helperPrefix string
		errorPrefix  string
		expected     string
	}{
		{name: "default preset", helperPrefix: "sgl", errorPrefix: "sgl", expected: "ErrSglUnknownColumn"},
		{name: "white-label preset", helperPrefix: "acme", errorPrefix: "acme", expected: "ErrAcmeUnknownColumn"},
		{name: "error prefix differs from helper prefix", helperPrefix: "acme", errorPrefix: "acmeerr", expected: "ErrAcmeerrUnknownColumn"},
		{name: "snake case error prefix", helperPrefix: "acme", errorPrefix: "acme_store", expected: "ErrAcmeStoreUnknownColumn"},
		{name: "initialism error prefix", helperPrefix: "acme", errorPrefix: "sql", expected: "ErrSQLUnknownColumn"},
		{name: "no error prefix", helperPrefix: "acme", errorPrefix: "", expected: "ErrUnknownColumn"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := testRequest()
			request.Style.HelperPrefix = testCase.helperPrefix
			request.Style.ErrorPrefix = testCase.errorPrefix

			assert.Equal(t, testCase.expected, NewVocabulary(request).ErrorValue("UnknownColumn"))
		})
	}
}

func TestVocabularySentinelWithoutHelperPrefix(t *testing.T) {
	t.Parallel()

	request := testRequest()
	request.Style.HelperPrefix = ""

	assert.Equal(t, "errX", NewVocabulary(request).Sentinel("X"))
}

func TestVocabularyDirectivesFollowRequestPrefixes(t *testing.T) {
	t.Parallel()

	request := testRequest()
	request.DirectivePrefixes = []string{"acme", "acmedb"}
	directives := NewVocabulary(request).Directives()

	assert.Equal(t, "acme", directives.Canonical)
	assert.Equal(t, []string{"acmedb"}, directives.Aliases)

	request.DirectivePrefixes = nil
	assert.Empty(t, NewVocabulary(request).Directives().Canonical)
}

func TestVocabularyScanIntoCallNamesTheAdapterByTheQualifierItsFileImportsItUnder(t *testing.T) {
	t.Parallel()

	adapter := sqlmodel.GoType{Package: "example.com/acme/json", Name: "ScanInto"}
	testCases := []struct {
		plan       map[string]string
		name       string
		adapter    sqlmodel.GoType
		want       string
		wantImport string
	}{
		{name: "the inline adapter needs no import", adapter: sqlmodel.GoType{}, plan: map[string]string{}, want: "sglScanInto(&row.Tags)"},
		{
			name: "an alias the plan gives the adapter's package", adapter: adapter,
			plan: map[string]string{"encoding/json": "json", "example.com/acme/json": "acmejson"},
			want: "acmejson.ScanInto(&row.Tags)", wantImport: `acmejson "example.com/acme/json"`,
		},
		{
			name: "a package the plan does not hold, named like a planned one", adapter: adapter,
			plan: map[string]string{"encoding/json": "json"},
			want: "json2.ScanInto(&row.Tags)", wantImport: `json2 "example.com/acme/json"`,
		},
		{
			name: "a package named as its path says", adapter: sqlmodel.GoType{Package: "example.com/lib/jsonx", Name: "Into"},
			plan: map[string]string{"encoding/json": "json"},
			want: "jsonx.Into(&row.Tags)", wantImport: `"example.com/lib/jsonx"`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := testRequest()
			request.Style.JSONScanInto = testCase.adapter
			tracker := goast.NewPlannedImportTracker(testCase.plan)
			target := goastutil.AddressExpr(goastutil.SelectorExpr("row", "Tags"))

			call := NewVocabulary(request).ScanIntoCall(target, tracker)

			declaration := &ast.FuncDecl{Name: ast.NewIdent("scan"), Type: &ast.FuncType{}, Body: goastutil.BlockStmt(&ast.ExprStmt{X: call})}
			source, err := goast.FormatFileWithAST("db", tracker, []ast.Decl{declaration})
			require.NoError(t, err)
			assert.Contains(t, string(source), testCase.want)
			if testCase.wantImport == "" {
				assert.NotContains(t, string(source), "import")
				return
			}
			assert.Contains(t, string(source), "import "+testCase.wantImport)
		})
	}
}

func TestVocabularyWrapsErrorsAndKeepsDocCommentsAsTheStyleSays(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		wraps       bool
		docComments bool
	}{
		{name: "labelled errors and doc comments", wraps: true, docComments: true},
		{name: "neither", wraps: false, docComments: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := testRequest()
			request.Style.HelperPrefix = "acme"
			request.Style.WrapErrors = testCase.wraps
			request.Style.DocComments = testCase.docComments
			vocabulary := NewVocabulary(request)

			require.Equal(t, testCase.wraps, vocabulary.WrapsErrors())
			require.Equal(t, testCase.docComments, vocabulary.DocComments())
			wrapped := vocabulary.WrapError(goastutil.StrLit("acme: GetBoat"), goastutil.CachedIdent("err"))
			require.Equal(t, `acmeWrapError("acme: GetBoat", err)`, renderExpression(t, wrapped))
		})
	}
}
