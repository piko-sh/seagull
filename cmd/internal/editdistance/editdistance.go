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

package editdistance

import "unicode/utf8"

// maxMeasuredRunes is the longest text Distance compares rune by rune; longer text is
// treated as too far from any name to be worth suggesting.
const maxMeasuredRunes = 1024

// Distance returns the Levenshtein distance between two strings, counted in runes, or
// limit when the distance is limit or more.
//
// The work is bounded: text longer than maxMeasuredRunes runes, or two texts whose
// lengths differ by limit or more, give limit without being compared.
//
// Takes left (string) which is one string.
// Takes right (string) which is the other.
// Takes limit (int) which is the smallest distance that is too far to matter.
//
// Returns int which is the number of single-rune edits between them, at most limit.
func Distance(left, right string, limit int) int {
	if limit <= 0 {
		return 0
	}
	leftLength, rightLength := utf8.RuneCountInString(left), utf8.RuneCountInString(right)
	if leftLength > maxMeasuredRunes || rightLength > maxMeasuredRunes || absolute(leftLength-rightLength) >= limit {
		return limit
	}
	return min(levenshtein([]rune(left), []rune(right)), limit)
}

// levenshtein is the edit distance between two rune sequences.
//
// Takes left ([]rune) which is one sequence.
// Takes right ([]rune) which is the other.
//
// Returns int which is the number of single-rune edits between them.
func levenshtein(left, right []rune) int {
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for column := range previous {
		previous[column] = column
	}
	for row, leftRune := range left {
		current[0] = row + 1
		for column, rightRune := range right {
			cost := 1
			if leftRune == rightRune {
				cost = 0
			}
			current[column+1] = min(previous[column+1]+1, current[column]+1, previous[column]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(right)]
}

// absolute returns the magnitude of a difference.
//
// Takes value (int) which may be negative.
//
// Returns int which is value without its sign.
func absolute(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
