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
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

const (
	replicaWait   = 5 * time.Second
	shortDeadline = 20 * time.Millisecond
)

var errFirstBootFailed = errors.New("the replica could not apply its migrations")

func TestRaceUpReleasesEveryReplicaTogether(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		replicas int
	}{
		{name: "one replica", replicas: 1},
		{name: "four replicas", replicas: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var arrived atomic.Int64
			everyone := make(chan struct{})
			boots := make([]Boot, test.replicas)
			for replica := range boots {
				boots[replica] = func(ctx context.Context) (int, error) {
					if arrived.Add(1) == int64(test.replicas) {
						close(everyone)
					}
					select {
					case <-everyone:
						return replica + 1, nil
					case <-time.After(replicaWait):
						return 0, errFirstBootFailed
					case <-ctx.Done():
						return 0, ctx.Err()
					}
				}
			}

			results := RaceUp(t, boots)

			require.Len(t, results, test.replicas)
			for replica, result := range results {
				require.Equal(t, ReplicaResult{Err: nil, Replica: replica, Applied: replica + 1}, result,
					"every replica runs while the others are still running")
			}
		})
	}
}

func TestRaceUpNamesTheDeadlineThatEndedARun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "a replica held past the deadline"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeoutCause(t.Context(), shortDeadline, errReplicaDeadline)
			defer cancel()
			held := func(ctx context.Context) (int, error) {
				select {
				case <-ctx.Done():
					return 0, ctx.Err()
				case <-time.After(replicaWait):
					return 0, errFirstBootFailed
				}
			}

			results := raceUp(ctx, []Boot{held})

			require.ErrorIs(t, results[0].Err, context.DeadlineExceeded)
			require.ErrorIs(t, results[0].Err, errReplicaDeadline)
		})
	}
}

func TestRaceUpReturnsAPanicAsTheReplicasError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "one replica panics and the other applies"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := seagull.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))
			panicking := func(context.Context) (int, error) { panic("the history table vanished") }
			applying := func(context.Context) (int, error) { return 2, nil }

			results := raceUp(ctx, []Boot{panicking, applying})

			require.ErrorIs(t, results[0].Err, seagull.ErrPanic)
			require.ErrorContains(t, results[0].Err, "replica 0 running its migrations")
			require.Equal(t, ReplicaResult{Err: nil, Replica: 1, Applied: 2}, results[1])
		})
	}
}

func TestReplicaProblemsDescribesEachFailedReplica(t *testing.T) {
	t.Parallel()

	results := []ReplicaResult{
		{Err: nil, Replica: 0, Applied: 2},
		{Err: errFirstBootFailed, Replica: 1, Applied: 0},
	}
	tests := []struct {
		name string
		hint string
		want []string
	}{
		{name: "without a hint", hint: "", want: []string{"replica 1 failed: the replica could not apply its migrations"}},
		{name: "with a hint", hint: "a deadline here means a lease blocked the change",
			want: []string{"replica 1 failed: the replica could not apply its migrations; a deadline here means a lease blocked the change"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, replicaProblems(results, test.hint))
		})
	}
}

func TestTotalAppliedAddsUpWhatTheReplicasApplied(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []ReplicaResult
		want    int
	}{
		{name: "no replicas", results: nil, want: 0},
		{name: "one replica applied everything", results: []ReplicaResult{{Replica: 0, Applied: 2}, {Replica: 1, Applied: 0}}, want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, TotalApplied(t, test.results, ""))
		})
	}
}
