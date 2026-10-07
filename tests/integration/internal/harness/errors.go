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

import "errors"

var (
	// errCaseTimeout is the cause of a case's context once caseTimeout has passed.
	errCaseTimeout = errors.New("the case did not finish within its time limit")

	// errTestDeadline is the cause of a case's context once go test's own deadline is near,
	// so the case stops and says why before go test panics.
	errTestDeadline = errors.New("the case stopped as go test's deadline drew near")

	// errReplicaDeadline is the cause of the context racing replicas share once
	// MigrationDeadline has passed.
	errReplicaDeadline = errors.New("the replicas did not finish their migrations within the deadline")

	// errStartupTimeout is the cause of the context a suite's server starts under once
	// startupTimeout has passed.
	errStartupTimeout = errors.New("the suite's database server did not start in time")

	// errShutdownTimeout is the cause of the context a suite's server stops under once
	// shutdownTimeout has passed.
	errShutdownTimeout = errors.New("the suite's database server did not stop in time")

	// errReplayTimeout is the cause of a migration replay's context once replayTimeout has
	// passed.
	errReplayTimeout = errors.New("the migration replay did not finish in time")

	// errCleanupTimeout is the cause of a clean-up's context once cleanupTimeout has passed.
	errCleanupTimeout = errors.New("the clean-up did not finish in time")

	// errUnknownEmitter reports a testspec.json that names an emitter the suite does not
	// accept.
	errUnknownEmitter = errors.New("testspec.json names an emitter the suite does not accept")

	// errTrailingData reports a JSON document followed by more than white space.
	errTrailingData = errors.New("the JSON document is followed by more data")
)
