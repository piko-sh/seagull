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

package methods

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

type stubBatch struct{}

func (stubBatch) Batch(query *gogen.Query) (ast.Decl, error) {
	return EmptyRowStruct(query.Naming.Method + "Batched"), nil
}

func (stubBatch) CopyFrom(query *gogen.Query) (ast.Decl, error) {
	return EmptyRowStruct(query.Naming.Method + "Copied"), nil
}

func (stubBatch) RepeatsRow(*gogen.Query) bool {
	return true
}

func (stubBatch) CopyStatement(*gogen.Query) gogen.CopyStatement {
	return gogen.CopyStatementAsIs
}

func purgeQuery(command sqlmodel.QueryCommand) *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:     "PurgeOldEvents",
		Filename: "events.sql",
		Command:  command,
		Template: template(text("DELETE FROM events")),
	}
}

func TestBuildAsyncExecMethod_HasErrorOnlySignature(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	query := queryFor(t, emission, purgeQuery(sqlmodel.QueryCommandAsyncExec))
	declaration := buildAsyncExecMethod(emission, query, StaticPrelude(query))
	require.NotNil(t, declaration)

	require.NotNil(t, declaration.Type.Results)
	require.Len(t, declaration.Type.Results.List, 1, "async exec returns only error")
}

func TestBuildAsyncExecMethod_BodyMatchesExecMethod(t *testing.T) {
	t.Parallel()

	asyncEmission := runtimeEmission()
	asyncQuery := queryFor(t, asyncEmission, purgeQuery(sqlmodel.QueryCommandAsyncExec))
	syncEmission := runtimeEmission()
	syncQuery := queryFor(t, syncEmission, purgeQuery(sqlmodel.QueryCommandExec))

	asyncDecl := buildAsyncExecMethod(asyncEmission, asyncQuery, StaticPrelude(asyncQuery))
	syncDecl := buildExecMethod(syncEmission, syncQuery, StaticPrelude(syncQuery))

	require.NotNil(t, asyncDecl)
	require.NotNil(t, syncDecl)

	assert.Equal(t, renderDecl(t, syncDecl), renderDecl(t, asyncDecl),
		"async-exec body must be identical to exec body")
}

func boatQuery(command sqlmodel.QueryCommand, optional bool) *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:          "GetBoat",
		Filename:      "boats.sql",
		Command:       command,
		Optional:      optional,
		ReadOnly:      command == sqlmodel.QueryCommandOne || command == sqlmodel.QueryCommandMany || command == sqlmodel.QueryCommandStream,
		Parameters:    []sqlmodel.QueryParameter{textParam("name", 1)},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("note", true)},
		Template:      template(text("SELECT id, note FROM boats WHERE name = "), parameter(0)),
	}
}

func TestMethodBuildsTheMethodItsCommandAsksFor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		batch   gogen.BatchBuilder
		name    string
		command sqlmodel.QueryCommand
		wants   []string
		absents []string
		option  bool
	}{
		{name: "one", command: sqlmodel.QueryCommandOne, wants: []string{
			"func (queries *Queries) GetBoat(ctx context.Context, name string) (GetBoatRow, error)",
			"queries.reader.QueryRowContext(ctx, getBoatSQL, name).Scan(&row.ID, &row.Note)",
			"return row, nil",
		}},
		{name: "an optional one", command: sqlmodel.QueryCommandOne, option: true, wants: []string{
			"func (queries *Queries) GetBoat(ctx context.Context, name string) (GetBoatRow, bool, error)",
			"if errors.Is(err, sql.ErrNoRows) {\n\t\treturn GetBoatRow{}, false, nil\n\t}",
			"return row, true, nil",
		}},
		{name: "many", command: sqlmodel.QueryCommandMany, wants: []string{
			"func (queries *Queries) GetBoat(ctx context.Context, name string) ([]GetBoatRow, error)",
			"rows, err := queries.reader.QueryContext(ctx, getBoatSQL, name)",
			"defer rows.Close()",
		}},
		{name: "stream", command: sqlmodel.QueryCommandStream, wants: []string{"yield"}},
		{name: "exec", command: sqlmodel.QueryCommandExec, wants: []string{
			"func (queries *Queries) GetBoat(ctx context.Context, name string) error",
			"_, err := queries.writer.ExecContext(ctx, getBoatSQL, name)",
			"return err",
		}},
		{name: "execresult", command: sqlmodel.QueryCommandExecResult, wants: []string{
			"func (queries *Queries) GetBoat(ctx context.Context, name string) (sql.Result, error)",
			"return queries.writer.ExecContext(ctx, getBoatSQL, name)",
		}},
		{name: "execrows", command: sqlmodel.QueryCommandExecRows, wants: []string{
			"func (queries *Queries) GetBoat(ctx context.Context, name string) (int64, error)",
			"results, err := queries.writer.ExecContext(ctx, getBoatSQL, name)",
			"return results.RowsAffected()",
		}},
		{name: "a batch on a driver with no batch builder", command: sqlmodel.QueryCommandBatch, wants: []string{
			"func (queries *Queries) GetBoat(ctx context.Context, params GetBoatParams) error",
			"_, err := queries.writer.ExecContext(ctx, getBoatSQL, params.Name)",
		}},
		{name: "a batch on a driver with a batch builder", command: sqlmodel.QueryCommandBatch, batch: stubBatch{}, wants: []string{"GetBoatBatched"}},
		{name: "a copyfrom on a driver with a batch builder", command: sqlmodel.QueryCommandCopyFrom, batch: stubBatch{}, wants: []string{"GetBoatCopied"}},
		{name: "an asynchronous mutation", command: sqlmodel.QueryCommandAsyncExec, wants: []string{"return err"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			driver := testDriver()
			driver.Batch = testCase.batch
			emission := testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), driver)
			query := queryFor(t, emission, boatQuery(testCase.command, testCase.option))

			method, err := Method(emission, query, StaticPrelude(query))
			require.NoError(t, err)
			source := renderDecl(t, method)
			for _, want := range testCase.wants {
				require.Contains(t, source, want)
			}
			for _, absent := range testCase.absents {
				require.NotContains(t, source, absent)
			}
		})
	}
}

func TestMethodRefusesAResultTheDriverDoesNotReport(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		command sqlmodel.QueryCommand
	}{
		{name: "execresult", command: sqlmodel.QueryCommandExecResult},
		{name: "execrows", command: sqlmodel.QueryCommandExecRows},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			driver := testDriver()
			driver.Connection.ExecResult = nil
			emission := testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), driver)
			query := queryFor(t, emission, boatQuery(testCase.command, false))

			method, err := Method(emission, query, StaticPrelude(query))

			require.Nil(t, method)
			require.Error(t, err)
		})
	}
}
