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
	"io/fs"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/sdk/migrate"
)

const (
	// MigrationDeadline bounds the migration runs a test races, so a lock that blocks a
	// schema change fails the test with the reason rather than waiting for the engine to
	// give up, such as CockroachDB after its five-minute descriptor lease.
	MigrationDeadline = time.Minute

	// concurrentReplicas is the number of migrators the concurrent first boot races.
	concurrentReplicas = 4

	// replicaOperation names a replica's run of its migrations when it panics.
	replicaOperation = "replica %d running its migrations"
)

// Boot is one replica's run of its migrations, such as a migrator's Up.
type Boot func(ctx context.Context) (applied int, err error)

// ReplicaResult is what one replica's run of its migrations reported.
type ReplicaResult struct {
	// Err is the run's error, wrapped with the context's cause when the deadline ended the
	// run, or a panicguard error when the run panicked.
	Err error

	// Replica is the replica's index among those raced.
	Replica int

	// Applied is the number of migrations the replica applied.
	Applied int
}

// Replicas builds count migrators of the same migrations on one database, as the replicas
// of a service would, every one before any of them runs, and returns their Up methods for
// RaceUp.
//
// Takes database (*sql.DB) which is the database the replicas share.
// Takes backend (migrate.Backend) which is the engine's backend.
// Takes files (fs.FS) which holds the migrations under migrations/.
// Takes count (int) which is the number of replicas.
//
// Returns []Boot which holds each replica's run.
func Replicas(t *testing.T, database *sql.DB, backend migrate.Backend, files fs.FS, count int) []Boot {
	t.Helper()

	boots := make([]Boot, 0, count)
	for range count {
		migrator, err := migrate.NewMigrator(database, backend, files, migrationsDirectory)
		require.NoError(t, err)
		boots = append(boots, migrator.Up)
	}
	return boots
}

// RaceUp releases every replica's run at once, as replicas booting together would, under
// one context that ends with a cause naming the deadline once MigrationDeadline has
// passed, waits for every one of them, and returns their results in replica order.
//
// Takes boots ([]Boot) which are the replicas' runs.
//
// Returns []ReplicaResult which holds each replica's result.
func RaceUp(t *testing.T, boots []Boot) []ReplicaResult {
	t.Helper()

	ctx, cancel := context.WithTimeoutCause(t.Context(), MigrationDeadline, errReplicaDeadline)
	defer cancel()
	return raceUp(ctx, boots)
}

// TotalApplied fails the test for each replica that failed, adding hint to the message
// when it is not empty, and returns the number of migrations the replicas applied between
// them.
//
// Takes results ([]ReplicaResult) which are the replicas' results.
// Takes hint (string) which says what a failure is likely to mean, or is empty.
//
// Returns int which is the total applied.
func TotalApplied(t *testing.T, results []ReplicaResult, hint string) int {
	t.Helper()

	problems := replicaProblems(results, hint)
	for _, problem := range problems {
		assert.Fail(t, problem)
	}
	if len(problems) > 0 {
		t.FailNow()
	}
	total := 0
	for _, result := range results {
		total += result.Applied
	}
	return total
}

// raceUp runs each boot in a goroutine of its own, all released together, under ctx, and
// returns each one's result in boot order once every one has finished.
//
// Takes boots ([]Boot) which are the replicas' runs.
//
// Returns []ReplicaResult which holds each replica's result.
func raceUp(ctx context.Context, boots []Boot) []ReplicaResult {
	start := make(chan struct{})
	finished := make(chan ReplicaResult, len(boots))
	var replicas sync.WaitGroup
	for replica, boot := range boots {
		replicas.Go(func() {
			<-start
			finished <- runReplica(ctx, replica, boot)
		})
	}
	close(start)
	replicas.Wait()
	close(finished)

	results := make([]ReplicaResult, len(boots))
	for result := range finished {
		results[result.Replica] = result
	}
	return results
}

// runReplica runs one replica's boot. A panic is returned as a panicguard error, its
// stack logged once, so one replica cannot end the test binary.
//
// Takes replica (int) which is the replica's index.
// Takes boot (Boot) which is its run.
//
// Returns ReplicaResult which is what the run reported.
func runReplica(ctx context.Context, replica int, boot Boot) ReplicaResult {
	applied, err := panicguard.RunValue(ctx, fmt.Sprintf(replicaOperation, replica), func() (int, error) {
		return boot(ctx)
	})
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("%w: %w", err, context.Cause(ctx))
	}
	return ReplicaResult{Err: err, Replica: replica, Applied: applied}
}

// replicaProblems describes each replica that failed.
//
// Takes results ([]ReplicaResult) which are the replicas' results.
// Takes hint (string) which is added to each description when it is not empty.
//
// Returns []string which holds one description per failed replica.
func replicaProblems(results []ReplicaResult, hint string) []string {
	var problems []string
	for _, result := range results {
		if result.Err == nil {
			continue
		}
		problem := "replica " + strconv.Itoa(result.Replica) + " failed: " + result.Err.Error()
		if hint != "" {
			problem += "; " + hint
		}
		problems = append(problems, problem)
	}
	return problems
}
