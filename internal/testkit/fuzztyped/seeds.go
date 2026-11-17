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

package fuzztyped

import "math"

const (
	// expressionSeeds is the number of expression seeds per mode.
	expressionSeeds = 9

	// updateStride separates a seed's update choice from its first row.
	updateStride = 3
)

// Seeds returns inputs that write every boundary value of every admitted family, filter
// by each and run every expression query, in every emitter mode.
//
// Takes engine (string) which names the dialect.
//
// Returns [][]byte which holds the seeds.
func Seeds(engine string) [][]byte {
	if !Supported(engine) {
		return nil
	}
	var seeds [][]byte
	usable := admitted(engine)
	for _, mode := range []Mode{ModeDatabaseSQL, ModePrepared, ModeNative} {
		if !ModeSupported(engine, mode) {
			continue
		}
		for index, entry := range usable {
			seeds = append(seeds, familySeeds(mode, index, entry)...)
		}
		for offset := range expressionSeeds {
			seed := []byte{Version, byte(mode), seedByte(len(usable)), maxRows - 1}
			for choice := range maxRows*expressionRowWidth + len(expressions)*expressionRowWidth {
				seed = append(seed, seedByte(choice*(offset+1)+offset))
			}
			seeds = append(seeds, seed)
		}
	}
	return seeds
}

// familySeeds writes each of a family's values first in turn and filters by it.
//
// Takes mode (Mode) which selects the emitter.
// Takes index (int) which is the family's position among the admitted families.
// Takes entry (*family) which is the family.
//
// Returns [][]byte which holds one seed per boundary value.
func familySeeds(mode Mode, index int, entry *family) [][]byte {
	seeds := make([][]byte, 0, len(entry.values))
	for start := range entry.values {
		seed := make([]byte, 0, choiceStart+maxRows+updateStride)
		seed = append(seed, Version, byte(mode), seedByte(index), maxRows-1)
		for row := range maxRows {
			seed = append(seed, seedByte(start+row))
		}
		seeds = append(seeds, append(seed, seedByte(2*start+1), seedByte(start), seedByte(start+updateStride)))
	}
	return seeds
}

// seedByte keeps the low byte of a small seed choice.
//
// Takes choice (int) which is a nonnegative choice.
//
// Returns byte which holds its low eight bits.
func seedByte(choice int) byte {
	return byte(choice & math.MaxUint8)
}
