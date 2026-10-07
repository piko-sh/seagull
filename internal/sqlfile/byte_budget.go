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

package sqlfile

import (
	"fmt"
	"sync"

	"seagull.sh/seagull/internal/fault"
)

// ByteBudget caps the bytes the readers sharing it read in total, such as every migration
// and query file of one generation. It is safe for concurrent use.
type ByteBudget struct {
	// limit is the most bytes the readers may read.
	limit int64

	// spent is the bytes read so far.
	spent int64

	// mu guards spent.
	mu sync.Mutex
}

// NewByteBudget creates a budget of limit bytes.
//
// Takes limit (int64) which is the most bytes the readers sharing the budget may read; a
// non-positive value means no cap.
//
// Returns *ByteBudget which starts with nothing spent.
func NewByteBudget(limit int64) *ByteBudget {
	return &ByteBudget{limit: limit, spent: 0, mu: sync.Mutex{}}
}

// spend charges a file's bytes to the budget.
//
// Takes filePath (string) which names the file in the error.
// Takes size (int64) which is the file's size in bytes.
//
// Returns error which wraps fault.ErrLimit when the file takes the total past the cap.
//
// Concurrency: the budget mutex serialises charges and limit checks.
func (budget *ByteBudget) spend(filePath string, size int64) error {
	if budget == nil || budget.limit <= 0 {
		return nil
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.spent+size > budget.limit {
		return fmt.Errorf("reading %q would take the input past %d bytes in total: %w", filePath, budget.limit, fault.ErrLimit)
	}
	budget.spent += size
	return nil
}
