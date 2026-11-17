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

package harness

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/emit"
)

// checkFuzzPreparedParameters compares inference with PostgreSQL preparation metadata.
//
// Takes t (*testing.T) which reports missing or different parameter types.
// Takes database (*sql.DB) which owns the isolated input schema.
// Takes suite (FuzzSuite) which supplies PostgreSQL type parsing.
// Takes model (fuzzcase.Model) which supplies independent binding values.
// Takes query (*emit.AnalysedQuery) which holds inferred parameters.
func checkFuzzPreparedParameters(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, model fuzzcase.Model, query *emit.AnalysedQuery) {
	t.Helper()
	sqlText, _ := fuzzRender(t, suite, model, query, query.Template)
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	connection, err := database.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = connection.Close() }()
	_, err = connection.ExecContext(ctx, "PREPARE seagull_fuzz_parameters AS "+sqlText)
	require.NoError(t, err, "prepare generated query for parameter metadata\n%s", sqlText)
	defer releaseFuzzPreparedStatement(t, connection)
	rows, err := connection.QueryContext(ctx, `SELECT parameter_type::text
        FROM pg_prepared_statements,
             unnest(parameter_types) WITH ORDINALITY AS p(parameter_type, position)
        WHERE name = 'seagull_fuzz_parameters'
        ORDER BY position`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	require.Len(t, names, len(query.Parameters), "prepared parameter count differs from inference\n%s", sqlText)
	for number, name := range names {
		parameter := fuzzParameterByNumber(query.Parameters, number+1)
		require.NotNil(t, parameter, "no inferred parameter for prepared position %d", number+1)
		checkFuzzType(t, suite, parameter.SQLType, name, fmt.Sprintf("parameter %d", number+1))
	}
}

// fuzzParameterByNumber finds an identity without relying on report slice order.
//
// Takes parameters ([]emit.QueryParameter) which contains inferred parameters.
// Takes number (int) which is the database's positional identity.
//
// Returns *emit.QueryParameter which is nil if the identity is absent.
func fuzzParameterByNumber(parameters []emit.QueryParameter, number int) *emit.QueryParameter {
	index := slices.IndexFunc(parameters, func(parameter emit.QueryParameter) bool { return parameter.Number == number })
	if index < 0 {
		return nil
	}
	return &parameters[index]
}

// releaseFuzzPreparedStatement removes metadata before releasing its pinned connection.
//
// Takes t (*testing.T) which reports failed cleanup.
// Takes connection (*sql.Conn) which owns the prepared statement.
func releaseFuzzPreparedStatement(t *testing.T, connection *sql.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(context.WithoutCancel(t.Context()), scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	_, err := connection.ExecContext(ctx, "DEALLOCATE seagull_fuzz_parameters")
	assert.NoError(t, err, "release prepared parameter probe")
}
