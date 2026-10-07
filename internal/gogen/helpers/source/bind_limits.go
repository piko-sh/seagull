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

package source

import (
	"errors"
	"fmt"
	"strings"
)

// sglMaxBindVariables is the engine's per-statement bind-variable cap. The slice splicer
// and the runtime builder both return ErrSglTooManyBindVariables when a statement would
// bind more values than this so a misshaped caller surfaces a developer-friendly
// diagnostic rather than a silent driver-level truncation or a hard wire-protocol error.
//
//sgl:data maxBindVariables
const sglMaxBindVariables = 65535

// ErrSglTooManyBindVariables is returned, wrapped, when a statement would bind more
// values than sglMaxBindVariables, such as for an oversized slice parameter. It is
// exported so callers can match it with errors.Is to distinguish an oversized bind list
// from a driver-level failure.
var ErrSglTooManyBindVariables = errors.New("sgl: too many bind variables")

// sglEdit replaces the bytes [Start, End) of a query with Text; an edit whose Start
// equals its End inserts Text there.
type sglEdit struct {
	// Text is the replacement or inserted SQL.
	Text string

	// Start is the first replaced byte offset in the original SQL.
	Start int

	// End is the exclusive replaced byte offset, equal to Start for an insertion.
	End int
}

// sglCheckBinds returns ErrSglTooManyBindVariables, wrapped with the count and the limit,
// when a statement would bind more values than sglMaxBindVariables.
//
// Takes count (int) which is the total number of bound arguments.
//
// Returns error when the statement exceeds the dialect's bind-variable limit.
func sglCheckBinds(count int) error {
	if count > sglMaxBindVariables {
		return fmt.Errorf("sgl: a statement of %d bind variables exceeds the limit of %d: %w", count, sglMaxBindVariables, ErrSglTooManyBindVariables)
	}
	return nil
}

// sglSplice returns query with its edits made. The generator lists the edits in the order
// of their offsets, which index query as it is written, so no edit moves another.
//
// Takes query (string) which is the original SQL text.
// Takes edits (...[]sglEdit) which holds replacement groups in ascending source-offset
// order.
//
// Returns string which is the edited SQL, or query unchanged when no edits exist.
func sglSplice(query string, edits ...[]sglEdit) string {
	size, count := len(query), 0
	for _, group := range edits {
		for _, edit := range group {
			size += len(edit.Text) - (edit.End - edit.Start)
			count++
		}
	}
	if count == 0 {
		return query
	}
	var builder strings.Builder
	builder.Grow(size)
	cursor := 0
	for _, group := range edits {
		for _, edit := range group {
			builder.WriteString(query[cursor:edit.Start])
			builder.WriteString(edit.Text)
			cursor = edit.End
		}
	}
	builder.WriteString(query[cursor:])
	return builder.String()
}
