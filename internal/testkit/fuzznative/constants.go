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

const (
	// kindSigned identifies a signed integer without rounding.
	kindSigned = "signed"

	// kindDecimal identifies an exact coefficient and scale.
	kindDecimal = "decimal"

	// kindJSON identifies an independent JSON document.
	kindJSON = "json"

	// kindUnsigned identifies an unsigned integer without rounding.
	kindUnsigned = "unsigned"

	// kindFloat identifies exact IEEE 754 bits.
	kindFloat = "float"

	// kindBoolean identifies a truth value spelled as zero or one.
	kindBoolean = "boolean"

	// kindText identifies a character string.
	kindText = "text"

	// kindUUID identifies a UUID compared without regard to case.
	kindUUID = "uuid"

	// kindBinary identifies a byte string.
	kindBinary = "binary"

	// kindDate identifies a calendar date.
	kindDate = "date"

	// kindTimestamp identifies a wall-clock timestamp.
	kindTimestamp = "timestamp"

	// kindTimestampTZ identifies an instant.
	kindTimestampTZ = "timestamptz"

	// kindArray identifies an ordered integer array.
	kindArray = "array"

	// numberOne is the canonical wire spelling of true.
	numberOne = "1"

	// numberNegative identifies a negative numeric spelling.
	numberNegative = "-"

	// numberZero is the canonical wire spelling of numeric zero.
	numberZero = "0"

	// initialFrameBytes bounds the first allocation for a protocol frame.
	initialFrameBytes = 4096

	// nativeRowBound stops an unexpectedly large generated result.
	nativeRowBound = 512
)
