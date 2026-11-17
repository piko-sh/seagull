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

package fuzzcase

const (
	// decimalBase selects exact decimal integer spelling.
	decimalBase = 10

	// scalarBitWidth bounds scalar integers and floating-point values.
	scalarBitWidth = 64

	// typedSigned names the signed value family.
	typedSigned = "signed"

	// typedUnsigned names the unsigned value family.
	typedUnsigned = "unsigned"

	// typedDecimal names the decimal value family.
	typedDecimal = "decimal"

	// typedFloat names the float value family.
	typedFloat = "float"

	// typedBoolean names the boolean value family.
	typedBoolean = "boolean"

	// typedText names the text value family.
	typedText = "text"

	// typedBinary names the binary value family.
	typedBinary = "binary"

	// typedDate names the date value family.
	typedDate = "date"

	// typedTimestamp names the timestamp value family.
	typedTimestamp = "timestamp"

	// typedTimestamptz names the timestamptz value family.
	typedTimestamptz = "timestamptz"

	// typedUUID names the uuid value family.
	typedUUID = "uuid"

	// typedJSON names the json value family.
	typedJSON = "json"

	// typedArray names the array value family.
	typedArray = "array"

	// typedScalarBound limits textual and binary payloads.
	typedScalarBound = 256

	// typedNumberBound limits exact numeric spellings.
	typedNumberBound = 128

	// typedCollectionBound limits elements in a collection.
	typedCollectionBound = 8

	// typedScaleBound limits decimal scale in either direction.
	typedScaleBound = 38

	// typedTimestampLayout preserves the supported fractional timestamp precision.
	typedTimestampLayout = "2006-01-02 15:04:05.999999999"

	// typedUTCTimestampLayout writes ClickHouse UTC values at microsecond precision.
	typedUTCTimestampLayout = "2006-01-02 15:04:05.999999"

	// typedMinus prefixes a negative numeric coefficient.
	typedMinus = "-"

	// typedQuote encloses an independently escaped literal.
	typedQuote = "'"

	// typedZero supplies an exact zero coefficient.
	typedZero = "0"

	// typedJSONType names the native JSON declaration.
	typedJSONType = "JSON"
)

const (
	// scenarioPositionCount counts the stable expression positions.
	scenarioPositionCount = 8

	// scenarioWrapperCount counts the independent type-preserving wrappers.
	scenarioWrapperCount = 4

	// scenarioWrapperStart locates the first optional expression wrapper.
	scenarioWrapperStart = 5

	// scenarioWrapperEnd bounds the final optional expression wrapper.
	scenarioWrapperEnd = 8

	// scenarioNullFlag selects a SQL NULL value.
	scenarioNullFlag = 128

	// scenarioDecimalScale retains six fractional decimal digits.
	scenarioDecimalScale = 6

	// scenarioArrayCount bounds array lengths from empty through three elements.
	scenarioArrayCount = 4

	// scenarioRelationCount counts the initial semantic relation families.
	scenarioRelationCount = 4

	// scenarioReservedSeed preserves the reserved seed byte.
	scenarioReservedSeed = 3

	// scenarioValueByte locates the scalar value choice.
	scenarioValueByte = 3

	// scenarioRelationRowsByte locates the relation row count.
	scenarioRelationRowsByte = 4

	// scenarioRelationRowsSeed seeds the maximum relation row count.
	scenarioRelationRowsSeed = 8

	// scenarioClose closes independently rendered SQL expressions.
	scenarioClose = ")"

	// scenarioCast names a type conversion node.
	scenarioCast = "cast"

	// historyCount counts the supported schema history families.
	historyCount = 6

	// historyRowsSeed seeds a populated schema history.
	historyRowsSeed = 7

	// historyValueSeed seeds a non-trivial historical value.
	historyValueSeed = 5

	// historyNullPeriod spaces SQL NULL values through a history.
	historyNullPeriod = 3

	// historyScoreRange bounds the independent historical score range.
	historyScoreRange = 33

	// historyScoreOffset centres historical scores around zero.
	historyScoreOffset = 16

	// typedMaxDepth bounds nested collection depth.
	typedMaxDepth = 2

	// aliasInteger names the native integer storage family.
	aliasInteger = "integer"

	// aliasWidth8 retains a native 8-bit representation.
	aliasWidth8 = 8

	// aliasWidth16 retains a native 16-bit representation.
	aliasWidth16 = 16

	// aliasWidth24 retains a native 24-bit representation.
	aliasWidth24 = 24

	// aliasWidth32 retains a native 32-bit representation.
	aliasWidth32 = 32

	// aliasWidth64 retains a native 64-bit representation.
	aliasWidth64 = 64

	// aliasWidth128 retains a native 128-bit representation.
	aliasWidth128 = 128
)

const (
	// positionLiteral selects the literal expression position.
	positionLiteral = iota

	// positionColumn selects the column expression position.
	positionColumn

	// positionParameter selects the parameter expression position.
	positionParameter

	// positionCoalesce selects the coalesce expression position.
	positionCoalesce

	// positionCase selects the case expression position.
	positionCase

	// positionParentheses selects the parentheses expression position.
	positionParentheses

	// positionSubquery selects the subquery expression position.
	positionSubquery

	// positionCast selects the cast expression position.
	positionCast
)

// relationProjection observes row identities and nullable scores.
const relationProjection = "SELECT i.id, i.score"

const (
	// historyViewLifecycle selects creation and removal of a dependent view.
	historyViewLifecycle = 3

	// historyIndexLifecycle selects creation and removal of an index.
	historyIndexLifecycle = 4
)
