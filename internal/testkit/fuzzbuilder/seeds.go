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

package fuzzbuilder

import "math"

const (
	// all, one, count, where, order, limit, offset and create spell seed calls.
	all, one, count = byte(OperationAll), byte(OperationOne), byte(OperationCount)

	// where adds a condition in a seed.
	where = byte(OperationWhere)

	// order adds an ordering in a seed.
	order = byte(OperationOrder)

	// limit sets the limit in a seed.
	limit = byte(OperationLimit)

	// offset sets the offset in a seed.
	offset = byte(OperationOffset)

	// create starts a new builder in a seed.
	create = byte(OperationNew)

	// seedQuery stands for the seeded query's index in a seed program.
	seedQuery = 0xFF

	// seedMissing stands for the first refused column spelling in a seed program.
	seedMissing = 0xFE

	// operatorStride spreads the operators over the columns of the column seeds.
	operatorStride = 5

	// valueChoices is the number of value bytes a condition reads.
	valueChoices = 3
)

// seedRows describes eight rows with repeated categories, NULL scores, NULL dotted
// values, peers with and without notes, and every label.
var seedRows = []byte{
	0x01, 0x83, 0x00, 0x05, 0x0E, 0x07, 0x0A, 0xC4, 0x0D, 0x02, 0x31, 0x13,
	0x1C, 0x8F, 0x08, 0x06, 0x22, 0x21, 0x2B, 0xC9, 0x0B, 0x13, 0x95, 0x1A,
}

// seedPrograms are call sequences every query's seeds run, in which seedQuery stands for
// the query's own index.
var seedPrograms = [][]byte{
	{all, count, one},
	{where, 0, 6, 3, 0, 0, order, 1, 4, order, 0, 0, limit, 4, offset, 2, all, count, one,
		where, 0, 10, 3, 3, 5, all, count, limit, 0, offset, 11, one},
	{where, seedMissing, 0, 0, 0, 0, all, create, seedQuery, 2, order, 0, 6, count,
		create, seedQuery, 3, where, 0, 12, 0, 0, 0, one, create, seedQuery, 0, all},
	{where, 0, 9, 0, 0, 0, byte(operationCount) + order, 0, 1, byte(operationCount) + all, all, count},
}

// Seeds returns positive sequences for every catalogue query and emitter mode.
//
// Takes engine (string) which names the dialect.
//
// Returns [][]byte which covers each column, operator, direction and terminal.
func Seeds(engine string) [][]byte {
	if !Supported(engine) {
		return nil
	}
	var seeds [][]byte
	for _, mode := range []Mode{ModeDatabaseSQL, ModePrepared, ModeNative} {
		if !ModeSupported(engine, mode) {
			continue
		}
		for index := range catalogue {
			seeds = append(seeds, querySeeds(byte(mode), byte(index))...)
		}
	}
	return seeds
}

// querySeeds covers one query in one mode.
//
// Takes mode (byte) which selects the emitter.
// Takes index (byte) which selects the query.
//
// Returns [][]byte which holds the query's seeds.
func querySeeds(mode, index byte) [][]byte {
	columns := len(catalogue[index].columns)
	header := func(parameter byte) []byte {
		data := make([]byte, 0, headerWidth+len(seedRows)+len(seedPrograms[1]))
		data = append(data, Version, mode, index, parameter, seedByte(len(seedRows)/rowWidth)|orphanFlag)
		return append(data, seedRows...)
	}
	seeds := make([][]byte, 0, len(seedPrograms)+columns)
	for parameter, program := range seedPrograms {
		seed := header(byte(parameter))
		for _, code := range program {
			switch code {
			case seedQuery:
				code = index
			case seedMissing:
				code = seedByte(columns + 1)
			}
			seed = append(seed, code)
		}
		seeds = append(seeds, seed)
	}
	for column := range columns {
		operator := seedByte((column*operatorStride + int(index)) % (len(operators) - refusedSpellings))
		direction := byte(column%(directionChoices-refusedSpellings) + directionChoices*(column%caseVariants))
		seed := append(header(byte(column)), where, byte(column), operator)
		for choice := range valueChoices {
			seed = append(seed, byte(column+choice+1))
		}
		seeds = append(seeds, append(seed, order, byte(column), direction, all, count, one))
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
