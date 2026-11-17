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

package fuzzruntime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDriverSelectsTheEmitterHarness(t *testing.T) {
	require.NotSame(t, Driver("pgx"), Driver("clickhouse"))
	require.NotNil(t, Driver("databasesql"))
	require.NotNil(t, Driver("databasesql_prepared"))
}

func TestSourcesFollowTheExecutionMode(t *testing.T) {
	require.Equal(t, workerSource(preparedSource), PreparedSource(preparedDriver))
	require.Equal(t, workerSource(directSource), PreparedSource("databasesql"))
	require.Equal(t, workerSource(sqlRows), RowsSource("databasesql"))
	require.NotContains(t, RowsSource("clickhouse"), "CLOSE_TYPE")
	require.NotContains(t, RowsSource("pgx"), "CLOSE_RETURN")
}

func TestWorkerSourcesBuildOnceCopied(t *testing.T) {
	t.Parallel()

	sources := []string{Program, PreparedSource(preparedDriver), PreparedSource("databasesql"),
		RowsSource("databasesql"), RowsSource("clickhouse"), RowsSource("pgx")}
	for _, source := range sources {
		require.NotContains(t, source, "//go:build")
		require.Contains(t, source, "package main")
	}
	require.Contains(t, programSource, workerBuildIgnore)
}
