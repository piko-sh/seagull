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

package goast

import (
	"strconv"
)

const (
	// firstNameSuffix is the first number written after a name that is already taken.
	firstNameSuffix = 2
)

// UniqueNames claims names unique among those it has claimed: a name itself when it is
// free, and otherwise the first of name2, name3 and so on that is.
//
// It remembers, for each name it had to number, the first number not yet tried, so
// claiming many alike names costs time linear in their number rather than quadratic. A
// claimed name is never released, so every number below the one remembered stays taken.
// UniqueNames is not safe for concurrent use.
type UniqueNames struct {
	// taken holds the names claimed.
	taken map[string]struct{}

	// next maps a name that had to be numbered to the first number not yet tried for it.
	next map[string]int
}

// NewUniqueNames returns an empty set of names, sized for a number of claims.
//
// Takes capacity (int) which is how many names are expected.
//
// Returns *UniqueNames which holds no name.
func NewUniqueNames(capacity int) *UniqueNames {
	return &UniqueNames{taken: make(map[string]struct{}, capacity), next: make(map[string]int)}
}

// Claim takes a name unique in the set: the name itself when it is free, and otherwise
// the first of name2, name3 and so on that is.
//
// Takes name (string) which is the name wanted.
//
// Returns string which is the name taken.
func (names *UniqueNames) Claim(name string) string {
	return ClaimUnique(name, names.next, names.take)
}

// take marks a name taken when it is free.
//
// Takes name (string) which is the name.
//
// Returns bool which is true when the name was free and is now taken.
func (names *UniqueNames) take(name string) bool {
	if _, taken := names.taken[name]; taken {
		return false
	}
	names.taken[name] = struct{}{}
	return true
}

// DisambiguateNames makes an ordered list of names unique, keeping the first of each name
// as it is and numbering the rest, as UniqueNames claims them.
//
// Takes names ([]string) which are the names in order.
//
// Returns []string which holds the unique names in the same order.
func DisambiguateNames(names []string) []string {
	unique := NewUniqueNames(len(names))
	result := make([]string, len(names))
	for index, name := range names {
		result[index] = unique.Claim(name)
	}
	return result
}

// ClaimUnique claims a name through take: the name itself when take accepts it, and
// otherwise the first of name2, name3 and so on take accepts, starting from the number
// next remembers for the name and remembering the one after the number taken.
//
// take must accept a name it has once refused never again, which holds for a set names
// are only ever added to, so no number below the one remembered can be free.
//
// Takes name (string) which is the name wanted.
// Takes next (map[string]int) which maps a numbered name to the first number not yet
// tried for it; it is updated.
// Takes take (func(string) bool) which takes a name when it is free and reports whether
// it did.
//
// Returns string which is the name taken.
func ClaimUnique(name string, next map[string]int, take func(string) bool) string {
	if take(name) {
		return name
	}
	suffix := max(next[name], firstNameSuffix)
	candidate := name + strconv.Itoa(suffix)
	for !take(candidate) {
		suffix++
		candidate = name + strconv.Itoa(suffix)
	}
	next[name] = suffix + 1
	return candidate
}
