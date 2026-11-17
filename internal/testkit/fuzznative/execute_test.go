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

package fuzznative

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

var errFakeDone = errors.New("fake transaction is done")

type fakeRow struct {
	ID     int64
	Value  Null[int64]
	IsNull bool
}

type fakeInsert struct {
	ID    int64
	Value Null[int64]
}

type fakeQueries struct {
	rows *[]fakeRow
	done *bool
}

type fakeBuilder struct {
	queries *fakeQueries
	minimum int64
	limit   int
	offset  int
	empty   bool
}

func (q *fakeQueries) InsertSigned(ctx context.Context, parameters fakeInsert) error {
	if err := q.usable(ctx); err != nil {
		return err
	}
	*q.rows = append(*q.rows, fakeRow{ID: parameters.ID, Value: parameters.Value, IsNull: !parameters.Value.Valid})
	return nil
}

func (q *fakeQueries) ReadSigned(ctx context.Context) ([]fakeRow, error) {
	if err := q.usable(ctx); err != nil {
		return nil, err
	}
	return slices.Clone(*q.rows), nil
}

func (q *fakeQueries) StreamSigned(ctx context.Context) func(func(fakeRow, error) bool) {
	return func(yield func(fakeRow, error) bool) {
		rows, err := q.ReadSigned(ctx)
		if err != nil {
			yield(fakeRow{}, err)
			return
		}
		for _, row := range rows {
			if !yield(row, nil) {
				return
			}
		}
	}
}

func (q *fakeQueries) BrowseSigned() *fakeBuilder {
	return &fakeBuilder{queries: q}
}

func (q *fakeQueries) usable(ctx context.Context) error {
	if *q.done {
		return errFakeDone
	}
	return ctx.Err()
}

func (b *fakeBuilder) Where(column, operator string, value any) *fakeBuilder {
	if list, ok := value.([]int64); ok && operator == "IN" && len(list) == 0 {
		b.empty = true
	} else if number, ok := value.(int64); ok && column == "id" {
		b.minimum = number
	}
	return b
}

func (b *fakeBuilder) Limit(limit int) *fakeBuilder {
	b.limit = limit
	return b
}

func (b *fakeBuilder) Offset(offset int) *fakeBuilder {
	b.offset = offset
	return b
}

func (b *fakeBuilder) filtered(ctx context.Context) ([]fakeRow, error) {
	rows, err := b.queries.ReadSigned(ctx)
	if err != nil || b.empty {
		return nil, err
	}
	return slices.DeleteFunc(rows, func(row fakeRow) bool { return row.ID < b.minimum }), nil
}

func (b *fakeBuilder) All(ctx context.Context) ([]fakeRow, error) {
	rows, err := b.filtered(ctx)
	if err != nil {
		return nil, err
	}
	rows = rows[min(max(b.offset, 0), len(rows)):]
	if b.limit > 0 {
		rows = rows[:min(b.limit, len(rows))]
	}
	return rows, nil
}

func (b *fakeBuilder) Count(ctx context.Context) (int64, error) {
	rows, err := b.filtered(ctx)
	return int64(len(rows)), err
}

func fakeSession() *Session {
	rows, done := []fakeRow{}, false
	queries := &fakeQueries{rows: &rows, done: &done}
	begin := func(context.Context) (*Transaction, error) {
		copied, finished := slices.Clone(rows), false
		inner := &fakeQueries{rows: &copied, done: &finished}
		end := func(keep bool) func(context.Context) error {
			return func(context.Context) error {
				if finished {
					return errFakeDone
				}
				finished = true
				if keep {
					rows = copied
				}
				return nil
			}
		}
		return &Transaction{Queries: inner, Commit: end(true), Rollback: end(false)}, nil
	}
	return &Session{Queries: queries, Close: func() error { return nil }, Begin: begin}
}

func signedModel() fuzzcase.RuntimeCase {
	values := make([]fuzzcase.TypedValue, 0, 4)
	for index := range 4 {
		value := fuzzcase.TypedNumber(kindSigned, strconv.Itoa(index*7-3), 0)
		value.Null = index == 2
		if value.Null {
			value.Number = numberZero
		}
		values = append(values, value)
	}
	return fuzzcase.RuntimeCase{Engine: "postgres", Kind: kindSigned, Values: values, Minimum: 2, Limit: 2, Offset: 1, Transactions: true}
}

func TestRunObservesACorrectGeneratedAPIWithoutProblems(t *testing.T) {
	t.Parallel()
	model := signedModel()
	response := Run(t.Context(), fakeSession(), model)
	require.Empty(t, response.Error, response.Stage)
	require.Empty(t, response.Panic)
	require.Empty(t, Problems(model, &response))
}

func TestRunStopsAtACancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response := Run(ctx, fakeSession(), signedModel())
	require.Equal(t, runStageNames()[0], response.Stage)
	require.NotEmpty(t, response.Error)
}

func TestRunRecoversAPanickingGeneratedAPI(t *testing.T) {
	t.Parallel()
	session := fakeSession()
	session.Queries = struct{ panicking }{}
	response := Run(t.Context(), session, signedModel())
	require.NotEmpty(t, response.Error+response.Panic)
}

type panicking struct{}

func TestServeAnswersEachRequestAndRedactsTheConnection(t *testing.T) {
	t.Parallel()
	model := signedModel()
	model.Data = []byte{12, 0, 3, 1, 0, 2, 1, 0}
	decoded, ok := fuzzcase.DecodeRuntime("postgres", model.Data)
	require.True(t, ok)
	factory := func(_ context.Context, connection string) (*Session, error) {
		if connection == "refuse" {
			return nil, errors.New("cannot open refuse")
		}
		return fakeSession(), nil
	}
	var input bytes.Buffer
	for index, connection := range []string{"valid", "refuse"} {
		frame, err := encodeFrame(Request{Connection: connection, Data: decoded.Data, ID: uint64(index + 1), Version: ProtocolVersion})
		require.NoError(t, err)
		input.Write(frame)
	}
	var output bytes.Buffer
	require.NoError(t, Serve("postgres", factory, &input, &output))
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)
	var refused Response
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &refused))
	require.Equal(t, "factory", refused.Stage)
	require.Equal(t, "cannot open [connection]", refused.Error)

	bad, err := encodeFrame(Request{Connection: "", Data: nil, ID: 0, Version: ProtocolVersion})
	require.NoError(t, err)
	require.ErrorIs(t, Serve("postgres", factory, bytes.NewReader(bad), &output), errInvalidEnvelope)
}

func TestServeModesRunsReadsThroughEachMode(t *testing.T) {
	t.Parallel()
	session := fakeSession()
	queries, ok := session.Queries.(*fakeQueries)
	require.True(t, ok)
	*queries.rows = []fakeRow{{ID: 1, Value: Null[int64]{V: 5, Valid: true}}}
	factories := map[string]Factory{"sql": func(context.Context, string) (*Session, error) { return session, nil }}
	calls := []ModeCall{{Method: "ReadSigned", Command: "many"}, {Method: "StreamSigned", Command: commandStream}, {Method: "Missing", Command: "many"}}
	var input bytes.Buffer
	for index, mode := range []string{"sql", "absent"} {
		frame, err := encodeFrame(ModeRequest{Mode: mode, Calls: calls, ID: uint64(index + 1), Version: ModesProtocolVersion})
		require.NoError(t, err)
		input.Write(frame)
	}
	var output bytes.Buffer
	require.NoError(t, ServeModes("postgres", factories, &input, &output))
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)
	var answered, absent ModeResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &answered))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &absent))
	require.Len(t, answered.Calls, 3)
	require.Equal(t, answered.Calls[0].Rows, answered.Calls[1].Rows)
	require.NotEmpty(t, answered.Calls[2].Error)
	require.Equal(t, "factory", absent.Stage)
}

func TestServeSequencesReportsRunnerFailures(t *testing.T) {
	t.Parallel()
	factory := func(context.Context, string) (*Session, error) { return fakeSession(), nil }
	runner := func(_ context.Context, _ *Session, data []byte) (any, error) {
		if len(data) == 0 {
			return nil, errors.New("empty input")
		}
		return []int{len(data)}, nil
	}
	var input bytes.Buffer
	for index, data := range [][]byte{{1, 2}, nil} {
		frame, err := encodeFrame(Request{Data: data, ID: uint64(index + 1), Version: ProtocolVersion})
		require.NoError(t, err)
		input.Write(frame)
	}
	var output bytes.Buffer
	require.NoError(t, ServeSequences(factory, runner, &input, &output))
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)
	var good, failed SequenceResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &good))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &failed))
	require.JSONEq(t, "[2]", string(good.Observations))
	require.Equal(t, "run", failed.Stage)
	require.Equal(t, "empty input", failed.Error)
}
