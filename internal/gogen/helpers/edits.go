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

package helpers

import (
	"cmp"
	"slices"
	"strings"
)

// edit replaces a range of the source.
type edit struct {
	// expand computes the replacement from the source it covers, given the extra edits in
	// force; when nil, replacement is used as it is.
	expand func(extra []edit) string

	// replacement is the text the range is replaced with.
	replacement string

	// start is the offset of the first byte replaced.
	start int

	// end is the offset just past the last byte replaced.
	end int
}

// text returns a range of the source with every edit inside it applied. An edit inside
// another is applied only when the outer edit renders the part of the source holding it.
//
// Takes start (int) which is the offset the range starts at.
// Takes end (int) which is the offset just past the range.
// Takes extra ([]edit) which holds further edits in force, such as the substitutions of
// an unrolled loop's variable.
//
// Returns string which is the edited text.
func (r *renderer) text(start, end int, extra []edit) string {
	var builder strings.Builder
	cursor := start
	for _, change := range r.editsWithin(start, end, extra) {
		if change.start < cursor {
			continue
		}
		builder.Write(r.source[cursor:change.start])
		if change.expand != nil {
			builder.WriteString(change.expand(extra))
		} else {
			builder.WriteString(change.replacement)
		}
		cursor = change.end
	}
	builder.Write(r.source[cursor:end])
	return builder.String()
}

// editsWithin returns the edits that lie inside a range, sorted.
//
// Takes start (int) which is the offset the range starts at.
// Takes end (int) which is the offset just past the range.
// Takes extra ([]edit) which holds further edits in force.
//
// Returns []edit which holds the edits inside the range.
func (r *renderer) editsWithin(start, end int, extra []edit) []edit {
	var within []edit
	for _, changes := range [][]edit{r.edits, extra} {
		for _, change := range changes {
			if change.start >= start && change.end <= end {
				within = append(within, change)
			}
		}
	}
	sortEdits(within)
	return within
}

// sortEdits orders edits by start, an outer edit before the edits it contains.
//
// Takes edits ([]edit) which are sorted in place.
func sortEdits(edits []edit) {
	slices.SortStableFunc(edits, func(left, right edit) int {
		return cmp.Or(cmp.Compare(left.start, right.start), cmp.Compare(right.end, left.end))
	})
}
