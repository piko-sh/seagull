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

package sqlexec

import (
	"errors"
	"fmt"

	"seagull.sh/seagull/internal/migrate"
)

var (
	// ErrLockLost is returned when the transaction that holds a table lock has ended while a
	// run still needs the lock, as when the server ended its idle session. Another process
	// may hold the lock by then, so the run stops before its next migration or seed.
	ErrLockLost = errors.New("the lock's transaction has ended, so another process may hold the lock")

	// errCheckpointFlushTimeout is the cause attached to the deadline of the context a
	// resume checkpoint is written on after a statement failed or the run was cancelled.
	errCheckpointFlushTimeout = errors.New("writing the resume checkpoint timed out")

	// errLockCheckTimeout is the cause attached to the deadline of the context that confirms
	// a table lock is still held.
	errLockCheckTimeout = errors.New("confirming the lock is still held timed out")

	// errInvalidExecutorOption is wrapped by the error an executor constructor returns for
	// an option given a value it cannot use.
	errInvalidExecutorOption = fmt.Errorf("invalid executor option: %w", migrate.ErrInvalidConfig)
)
