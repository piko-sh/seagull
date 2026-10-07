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

package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestASetOfTheSearchPathReplaysAsTheSearchPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantKind   ast.MutationKind
		wantSchema []string
		wantLocal  bool
	}{
		{name: "set to", sql: "SET search_path TO shop, public", wantKind: ast.MutationSetSearchPath, wantSchema: []string{"shop", "public"}},
		{name: "set equals strings and quoted names", sql: `SET search_path = 'Shop', "Archive", stock`,
			wantKind: ast.MutationSetSearchPath, wantSchema: []string{"Shop", "Archive", "stock"}},
		{name: "a bare name folds", sql: "SET search_path TO Shop", wantKind: ast.MutationSetSearchPath, wantSchema: []string{"shop"}},
		{name: "the user's schema is left out", sql: `SET search_path TO "$user", public`,
			wantKind: ast.MutationSetSearchPath, wantSchema: []string{"public"}},
		{name: "session", sql: "SET SESSION search_path TO shop", wantKind: ast.MutationSetSearchPath, wantSchema: []string{"shop"}},
		{name: "local", sql: "SET LOCAL search_path TO shop", wantKind: ast.MutationSetSearchPath, wantSchema: []string{"shop"}, wantLocal: true},
		{name: "set schema", sql: "SET SCHEMA 'shop'", wantKind: ast.MutationSetSearchPath, wantSchema: []string{"shop"}},
		{name: "set to default", sql: "SET search_path TO DEFAULT", wantKind: ast.MutationResetSearchPath},
		{name: "set local to default", sql: "SET LOCAL search_path = DEFAULT", wantKind: ast.MutationResetSearchPath, wantLocal: true},
		{name: "reset", sql: "RESET search_path", wantKind: ast.MutationResetSearchPath},
		{name: "reset all", sql: "RESET ALL", wantKind: ast.MutationResetSearchPath},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation)
			require.Equal(t, testCase.wantKind, mutation.Kind)
			require.Equal(t, testCase.wantSchema, mutation.SearchPath)
			require.Equal(t, testCase.wantLocal, mutation.Local)
		})
	}
}

func TestOtherSettingsLeaveTheCatalogueAlone(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SET statement_timeout = 0",
		"SET client_encoding = 'UTF8'",
		"SET search_path FROM CURRENT",
		"RESET statement_timeout",
		"SET LOCAL lock_timeout TO '5s'",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.Nil(t, applyDDL(t, sql))
		})
	}
}

func TestAMalformedSetOfTheSearchPathIsAnError(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{"SET search_path shop", "SET search_path TO", "SET search_path TO shop public"} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.Error(t, applyDDLExpectError(t, sql))
		})
	}
}
