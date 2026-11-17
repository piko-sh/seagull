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

package corpus

import (
	"bytes"
	"iter"
	"slices"
)

const (
	// commandModel is the version byte of a DML command sequence model.
	commandModel = 11

	// commandWidth is the byte width of one DML operation.
	commandWidth = 3

	// commandLimit is the number of operation counts a command model encodes.
	commandLimit = 16

	// commandKinds is the number of DML operation kinds.
	commandKinds = 6

	// highBit is the flag bit some model bytes keep while reducing.
	highBit = 128

	// lowSevenBits keeps a byte's value below its flag bit.
	lowSevenBits = 127

	// relationModel is the version byte of the model whose type bytes come in groups.
	relationModel = 13

	// relationGroup is the size of a type group in a relation model.
	relationGroup = 12

	// relationFlags masks the flag bits of a relation model's second byte.
	relationFlags = 3

	// flagIndex is the byte of a flagged model whose high bit is a mode.
	flagIndex = 3
)

// modelLayout is the fixed prefix and protected bytes of one model version.
type modelLayout struct {
	// protected holds the indices whose bytes never change.
	protected []int

	// prefix is the length of the header that cuts never remove.
	prefix int
}

var (
	// modelLayouts maps each model version to its header layout.
	modelLayouts = map[byte]modelLayout{
		1: {prefix: 8, protected: []int{0, 1, 2}}, 2: {prefix: 8, protected: []int{0, 1, 2}},
		3: {prefix: 8, protected: []int{0, 1, 2}}, 4: {prefix: 1, protected: []int{0}},
		5: {prefix: 8, protected: []int{0, 1, 2}}, 6: {prefix: 3, protected: []int{0, 1, 2}},
		7: {prefix: 3, protected: []int{0, 1, 2}}, 8: {prefix: 5, protected: []int{0, 1, 2}},
		9: {prefix: 5, protected: []int{0, 1}}, 10: {prefix: 4, protected: []int{0, 1}},
		12: {prefix: 8, protected: []int{0, 1, 4}}, 13: {prefix: 2, protected: []int{0}},
		128: {prefix: 2, protected: []int{0, 1}}, 129: {prefix: 4, protected: []int{0, 1, 2, 3}},
	}

	// flaggedModels are the model versions whose flagIndex byte keeps its high bit.
	flaggedModels = []byte{8, 12}

	// fixedStringTargets keep their string arguments, which name an engine or emitter.
	fixedStringTargets = []string{"FuzzEmit", "FuzzGeneratedCompile", "FuzzGeneratedRuntime"}
)

// Candidates proposes smaller inputs for a target, coarse cuts first.
//
// Engine and emitter names stay fixed, file bundles keep their envelope and models
// keep their version and execution-mode bytes.
//
// Takes arguments ([]Argument) which is the input to reduce.
// Takes function (string) which is the fuzz target's function name.
//
// Returns iter.Seq[[]Argument] which yields each candidate input lazily.
func Candidates(arguments []Argument, function string) iter.Seq[[]Argument] {
	return func(yield func([]Argument) bool) {
		for index, argument := range arguments {
			if argument.Kind == KindString && slices.Contains(fixedStringTargets, function) {
				continue
			}
			for candidate := range argumentCandidates(argument, function) {
				replaced := slices.Clone(arguments)
				replaced[index] = Argument{Kind: argument.Kind, Value: candidate}
				if !yield(replaced) {
					return
				}
			}
		}
	}
}

// Validate checks that Candidates can reduce an input for a target.
//
// Takes arguments ([]Argument) which is the input to reduce.
// Takes function (string) which is the fuzz target's function name.
//
// Returns error when a file bundle is invalid or a model lacks its version byte.
func Validate(arguments []Argument, function string) error {
	for _, argument := range arguments {
		if argument.Kind == KindString && slices.Contains(fixedStringTargets, function) {
			continue
		}
		if function == bundleTarget {
			if _, err := decodeBundle(argument.Value); err != nil {
				return err
			}
			continue
		}
		if argument.Kind == KindBytes && len(argument.Value) == 0 {
			return errEmptyModel
		}
	}
	return nil
}

// Complexity orders inputs by total length and then by their exact bytes.
//
// Takes left ([]Argument) and right ([]Argument) which have the same number of arguments.
//
// Returns int which is negative, zero or positive as left is simpler, equal or more complex.
func Complexity(left []Argument, right []Argument) int {
	if order := totalLength(left) - totalLength(right); order != 0 {
		return order
	}
	for index, argument := range left {
		if index >= len(right) {
			break
		}
		if order := bytes.Compare(argument.Value, right[index].Value); order != 0 {
			return order
		}
	}
	return len(left) - len(right)
}

// TotalLength returns the summed length of an input's arguments.
//
// Takes arguments ([]Argument) which is the input to measure.
//
// Returns int which is the number of payload bytes.
func TotalLength(arguments []Argument) int {
	return totalLength(arguments)
}

// argumentCandidates picks the reductions that suit one argument.
//
// Takes argument (Argument) which is the value to reduce.
// Takes function (string) which is the fuzz target's function name.
//
// Returns iter.Seq[[]byte] which yields each candidate value.
func argumentCandidates(argument Argument, function string) iter.Seq[[]byte] {
	if function == bundleTarget {
		return bundleCandidates(argument.Value)
	}
	if argument.Kind == KindBytes {
		return modelCandidates(argument.Value)
	}
	return rawCandidates(argument.Value)
}

// totalLength sums the lengths of an input's arguments.
//
// Takes arguments ([]Argument) which is the input to measure.
//
// Returns int which is the number of payload bytes.
func totalLength(arguments []Argument) int {
	total := 0
	for _, argument := range arguments {
		total += len(argument.Value)
	}
	return total
}

// cuts yields contiguous ranges to delete, whole length first and halving each round.
//
// Takes length (int) which is the number of items.
// Takes minimum (int) which is the fewest items a deletion may leave.
//
// Returns iter.Seq2[int, int] which yields each start and end.
func cuts(length int, minimum int) iter.Seq2[int, int] {
	return func(yield func(int, int) bool) {
		for width := length; width > 0; width /= 2 {
			for start := 0; start < length; start += width {
				end := min(length, start+width)
				if length-(end-start) < minimum {
					continue
				}
				if !yield(start, end) {
					return
				}
			}
		}
	}
}

// scalars yields smaller boundary values that keep the masked bits of value.
//
// Takes value (int) which is the byte being reduced.
// Takes mask (int) which holds the bits to keep.
//
// Returns iter.Seq[int] which yields each smaller value.
func scalars(value int, mask int) iter.Seq[int] {
	choices := []int{0, 1, 2, lowSevenBits, highBit, value / 2, value & lowSevenBits}
	slices.Sort(choices)
	choices = slices.Compact(choices)
	return func(yield func(int) bool) {
		for _, choice := range choices {
			candidate := (choice &^ mask) | (value & mask)
			if candidate < value && !yield(candidate) {
				return
			}
		}
	}
}

// withByte returns a copy of data with one byte replaced.
//
// Takes data ([]byte) which is the original.
// Takes index (int) which is the position to replace.
// Takes value (int) which is the new byte.
//
// Returns []byte which is the copy.
func withByte(data []byte, index int, value int) []byte {
	replaced := slices.Clone(data)
	replaced[index] = byte(value) //nolint:gosec // Every candidate is below an existing byte.
	return replaced
}

// without returns data with the range from start to end removed.
//
// Takes data ([]byte) which is the original.
// Takes start (int) and end (int) which bound the range.
//
// Returns []byte which is a new slice.
func without(data []byte, start int, end int) []byte {
	return slices.Concat(data[:start], data[end:])
}

// concat yields every candidate of each sequence in turn.
//
// Takes sequences (...iter.Seq[[]byte]) which are the sequences to join.
//
// Returns iter.Seq[[]byte] which yields them in order.
func concat(sequences ...iter.Seq[[]byte]) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for _, sequence := range sequences {
			for candidate := range sequence {
				if !yield(candidate) {
					return
				}
			}
		}
	}
}

// single yields one candidate.
//
// Takes candidate ([]byte) which is the candidate.
//
// Returns iter.Seq[[]byte] which yields it once.
func single(candidate []byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		yield(candidate)
	}
}

// rangeCuts yields data with each cut range removed, keeping a fixed prefix.
//
// Takes data ([]byte) which is the value to reduce.
// Takes prefix (int) which is the number of leading bytes never cut.
// Takes minimum (int) which is the fewest bytes after the prefix a cut may leave.
//
// Returns iter.Seq[[]byte] which yields each shortened value.
func rangeCuts(data []byte, prefix int, minimum int) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for start, end := range cuts(max(0, len(data)-prefix), minimum) {
			if !yield(without(data, prefix+start, prefix+end)) {
				return
			}
		}
	}
}

// byteChoices yields data with each byte from first to before last lowered to each choice.
//
// Takes data ([]byte) which is the value to reduce.
// Takes first (int) and last (int) which bound the bytes to lower.
// Takes choices (func(int, int) iter.Seq[int]) which gives the values for an index and byte.
//
// Returns iter.Seq[[]byte] which yields each changed value.
func byteChoices(data []byte, first int, last int, choices func(index int, value int) iter.Seq[int]) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for index := first; index < min(last, len(data)); index++ {
			for candidate := range choices(index, int(data[index])) {
				if !yield(withByte(data, index, candidate)) {
					return
				}
			}
		}
	}
}

// plainScalars yields the smaller boundary values of any byte.
//
// Takes value (int) which is the byte.
//
// Returns iter.Seq[int] which yields each smaller value.
func plainScalars(_ int, value int) iter.Seq[int] {
	return scalars(value, 0)
}

// rawCandidates removes whole lines, then byte ranges, then lowers single bytes.
//
// Takes data ([]byte) which is the value to reduce.
//
// Returns iter.Seq[[]byte] which yields each candidate.
func rawCandidates(data []byte) iter.Seq[[]byte] {
	return concat(lineCuts(data), rangeCuts(data, 0, 0), byteChoices(data, 0, len(data), plainScalars))
}

// lineCuts removes ranges of whole lines when there is more than one.
//
// Takes data ([]byte) which is the value to reduce.
//
// Returns iter.Seq[[]byte] which yields each shortened value.
func lineCuts(data []byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		lines := splitLinesKeepEnds(data)
		if len(lines) < 2 {
			return
		}
		for start, end := range cuts(len(lines), 0) {
			if !yield(bytes.Join(slices.Concat(lines[:start], lines[end:]), nil)) {
				return
			}
		}
	}
}

// splitLinesKeepEnds splits bytes after each \n, \r or \r\n, keeping the endings.
//
// Takes data ([]byte) which is the text to split.
//
// Returns [][]byte which holds the lines.
func splitLinesKeepEnds(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for index := 0; index < len(data); index++ {
		switch data[index] {
		case '\n':
		case '\r':
			if index+1 < len(data) && data[index+1] == '\n' {
				index++
			}
		default:
			continue
		}
		lines = append(lines, data[start:index+1])
		start = index + 1
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// commandCandidates drops whole DML operations, keeping the encoded count right, then
// lowers operation bytes and finally trims trailing bytes no operation reads.
//
// Takes data ([]byte) which is a command model of at least one byte.
//
// Returns iter.Seq[[]byte] which yields each candidate.
func commandCandidates(data []byte) iter.Seq[[]byte] {
	count := 1
	if len(data) > 1 {
		count = int(data[1])%commandLimit + 1
	}
	width := count * commandWidth
	return concat(
		operationCuts(data, count),
		byteChoices(data, 2, 2+width, commandByteChoices),
		single(slices.Clone(data[:min(len(data), 2+width)])),
	)
}

// operationCuts drops ranges of whole operations, always keeping one.
//
// Takes data ([]byte) which is a command model.
// Takes count (int) which is the number of operations it encodes.
//
// Returns iter.Seq[[]byte] which yields each shortened model.
func operationCuts(data []byte, count int) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		payload := make([]byte, count*commandWidth)
		if len(data) > 2 {
			copy(payload, data[2:])
		}
		for start, end := range cuts(count, 1) {
			remaining := slices.Concat(payload[:start*commandWidth], payload[end*commandWidth:])
			operations := len(remaining)/commandWidth - 1
			header := []byte{commandModel, byte(operations)} //nolint:gosec // At most commandLimit operations.
			if !yield(slices.Concat(header, remaining)) {
				return
			}
		}
	}
}

// commandByteChoices yields a smaller operation kind for an operation's first byte, and
// smaller values for the others, keeping the flag bit of its last byte.
//
// Takes index (int) which is the byte's position in the model.
// Takes value (int) which is the byte.
//
// Returns iter.Seq[int] which yields each smaller value.
func commandByteChoices(index int, value int) iter.Seq[int] {
	position := (index - 2) % commandWidth
	if position == 0 {
		return func(yield func(int) bool) {
			if candidate := value % commandKinds; candidate < value {
				yield(candidate)
			}
		}
	}
	mask := 0
	if position == 2 {
		mask = highBit
	}
	return scalars(value, mask)
}

// modelCandidates reduces a structured model while keeping its family and modes.
//
// Takes data ([]byte) which is a model.
//
// Returns iter.Seq[[]byte] which yields each candidate, or none for an empty model.
func modelCandidates(data []byte) iter.Seq[[]byte] {
	if len(data) == 0 {
		return concat()
	}
	version := data[0]
	if version == commandModel {
		return commandCandidates(data)
	}
	layout, found := modelLayouts[version]
	if !found {

		return concat(rangeCuts(data, 1, 0), byteChoices(data, 1, len(data), plainScalars))
	}
	minimum := 0
	if version == relationModel {
		minimum = 1
	}
	choices := func(index int, value int) iter.Seq[int] {
		if slices.Contains(layout.protected, index) {
			return func(func(int) bool) {}
		}
		return modelByteChoices(version, index, value)
	}
	return concat(rangeCuts(data, layout.prefix, minimum), byteChoices(data, 0, len(data), choices))
}

// modelByteChoices yields the smaller values one model byte may take.
//
// Takes version (byte) which is the model version.
// Takes index (int) which is the byte's position.
// Takes value (int) which is the byte.
//
// Returns iter.Seq[int] which yields each smaller value.
func modelByteChoices(version byte, index int, value int) iter.Seq[int] {
	if version == relationModel && index >= 2 {
		return func(yield func(int) bool) {
			for group := range value / relationGroup {
				if !yield(value%relationGroup + relationGroup*group) {
					return
				}
			}
		}
	}
	mask := 0
	if slices.Contains(flaggedModels, version) && index == flagIndex {
		mask = highBit
	}
	if version == relationModel && index == 1 {
		mask = relationFlags
	}
	return scalars(value, mask)
}
