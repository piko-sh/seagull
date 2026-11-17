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

package dialecttest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestBindingCountUsesPlaceholderIdentities(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			info := engine.Info()
			form := info.Placeholders.Execution
			first, repeated := form.Render(1, "a", "Int64"), form.Render(1, "a", "Int64")
			want := 1
			switch form.Kind {
			case dialect.PlaceholderAnonymous:
				want = 2
			case dialect.PlaceholderNumbered:
				first = form.Prefix + "01" + form.Suffix
			case dialect.PlaceholderNamed:
				first = form.Render(1, "a", " Int64")
			}
			tokens, err := significantTokens(info.Lexical, "SELECT "+first+", "+repeated)
			require.NoError(t, err)
			require.Equal(t, want, boundArguments(&info.Placeholders, tokens))
			tokens, err = significantTokens(info.Lexical, "SELECT "+first+", "+form.Render(2, "b", "Int64"))
			require.NoError(t, err)
			require.Equal(t, 2, boundArguments(&info.Placeholders, tokens))
		})
	}
}

func TestUnwrapDerivedRemovesOnlyTheBuilderWrapping(t *testing.T) {
	const inner = "SELECT a FROM x UNION SELECT a FROM y"
	wrapped := "SELECT * FROM (" + inner + ") AS result_rows LIMIT ?"
	compound := func(dynamic, runtime bool) *sqlmodel.AnalysedQuery {
		query := &sqlmodel.AnalysedQuery{IsDynamic: dynamic, DynamicRuntime: runtime}
		query.ResultShape.Compound = true
		return query
	}
	lastBranch := &dialect.Info{}
	lastBranch.Pagination.CompoundBindsLastBranch = true
	cases := []struct {
		name    string
		info    *dialect.Info
		query   *sqlmodel.AnalysedQuery
		sql     string
		want    string
		wrapped bool
	}{
		{name: "dynamic", info: lastBranch, query: compound(true, false), sql: wrapped, want: inner + " LIMIT ?", wrapped: true},
		{name: "runtime", info: lastBranch, query: compound(false, true), sql: wrapped, want: inner + " LIMIT ?", wrapped: true},
		{name: "static", info: lastBranch, query: compound(false, false), sql: wrapped, want: wrapped, wrapped: false},
		{name: "whole statement dialect", info: &dialect.Info{}, query: compound(true, false), sql: wrapped, want: wrapped, wrapped: false},
		{name: "not compound", info: lastBranch, query: &sqlmodel.AnalysedQuery{IsDynamic: true}, sql: wrapped, want: wrapped, wrapped: false},
		{name: "no wrapping", info: lastBranch, query: compound(true, false), sql: inner, want: inner, wrapped: false},
		{name: "picked rows", info: &dialect.Info{}, query: &sqlmodel.AnalysedQuery{DynamicRuntime: true, ReadsResultRows: true},
			sql: "SELECT * FROM (SELECT a FROM x LIMIT 3) AS result_rows LIMIT ?", want: "SELECT a FROM x LIMIT 3 LIMIT ?", wrapped: true},
		{name: "picked rows of a query that is not a runtime builder", info: &dialect.Info{}, query: &sqlmodel.AnalysedQuery{ReadsResultRows: true},
			sql: wrapped, want: wrapped, wrapped: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, wrapped := unwrapDerived(testCase.info, testCase.query, testCase.sql)
			require.Equal(t, testCase.want, got)
			require.Equal(t, testCase.wrapped, wrapped)
		})
	}
}

func TestPlaceholderIdentityKeepsUnreadablePlaceholders(t *testing.T) {
	info := postgres.New().Info()
	require.Equal(t, "1", placeholderIdentity(&info.Placeholders, "$01"))
	require.Equal(t, "?", placeholderIdentity(&info.Placeholders, "?"))
}
