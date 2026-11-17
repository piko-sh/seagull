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
	"sync"
)

const (
	// diagnosticBytes bounds retained stderr independently of the native protocol.
	diagnosticBytes = 64 << 10

	// diagnosticDropped marks a tail whose earlier output was discarded.
	diagnosticDropped = "[earlier helper output dropped] "
)

// diagnosticTail retains a bounded diagnostic suffix while the helper is running.
type diagnosticTail struct {
	// retained holds only the latest output within the diagnostic bound.
	retained []byte

	// mutex coordinates the command's copy goroutine and request failures.
	mutex sync.Mutex

	// dropped records that older output was discarded to keep the bound.
	dropped bool
}

// Write retains the latest diagnostic bytes without blocking on a consumer.
//
// Takes data ([]byte) which contains the helper's latest standard error output.
//
// Returns int which reports complete consumption even when old output is discarded.
// Returns error which is always nil.
//
// Concurrency: A mutex protects the retained bytes.
func (tail *diagnosticTail) Write(data []byte) (int, error) {
	tail.mutex.Lock()
	defer tail.mutex.Unlock()
	count := len(data)
	data = data[max(0, len(data)-diagnosticBytes):]
	keep := min(len(tail.retained), diagnosticBytes-len(data))
	if keep < len(tail.retained) || len(data) < count {
		tail.dropped = true
	}
	copy(tail.retained, tail.retained[len(tail.retained)-keep:])
	tail.retained = append(tail.retained[:keep], data...)
	return count, nil
}

// String snapshots the bounded diagnostic suffix.
//
// After dropped output the snapshot starts at the next whole line, so a connection string
// cut in half cannot escape redaction.
//
// Returns string which still requires connection redaction before reporting. It starts
// with a marker when earlier output was dropped.
//
// Concurrency: A mutex protects the snapshot.
func (tail *diagnosticTail) String() string {
	tail.mutex.Lock()
	defer tail.mutex.Unlock()
	if !tail.dropped {
		return string(tail.retained)
	}
	_, whole, _ := bytes.Cut(tail.retained, []byte{'\n'})
	return diagnosticDropped + string(whole)
}
