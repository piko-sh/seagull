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

package emittest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMainSourceCompilesTheHarnessIntoPackageMain(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		source   string
		expected string
	}{
		{name: "a package clause is replaced", source: "// Package recorder records.\npackage recorder\n\nfunc Connect() {}\n",
			expected: "// Package recorder records.\npackage main\n\nfunc Connect() {}\n"},
		{name: "a missing package clause is added", source: "func Connect() {}\n", expected: "package main\n\nfunc Connect() {}\n"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			harness := &Harness{Name: "test", Source: testCase.source}
			require.Equal(t, testCase.expected, harness.mainSource())
		})
	}
}

func TestDatabaseSQLHarnessDeclaresTheContract(t *testing.T) {
	t.Parallel()

	source := DatabaseSQL().mainSource()

	require.Contains(t, source, "\npackage main\n")
	require.Contains(t, source, "\ntype Statement struct {")
	require.Contains(t, source, "\nfunc Connect() *sql.DB {")
	require.Contains(t, source, "\nfunc Take() []Statement {")
	require.Empty(t, DatabaseSQL().Modules)
}
