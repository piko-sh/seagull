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

package dialectkit

import "fmt"

// parameterNumberBase is the radix of the digits of a numbered placeholder.
const parameterNumberBase = 10

// ReadParameterNumber reads the decimal digits of a numbered placeholder, such as the 3
// of $3 or ?3, stopping as soon as the number passes maximum so a long run of digits
// cannot overflow.
//
// Takes input (string) which is the source being scanned.
// Takes start (int) which is the offset of the placeholder's sigil, where an error is
// placed.
// Takes position (int) which is the offset of the first byte that may be a digit.
// Takes maximum (int) which is the highest number a placeholder may carry.
//
// Returns end (int) which is the offset after the digits, or position when none begins
// there.
// Returns number (int) which is the number the digits spell, or 0 when there are none.
// Returns err (error) which is a ParseError from start to the digit that took the number
// past maximum, or nil.
func ReadParameterNumber(input string, start, position, maximum int) (end int, number int, err error) {
	end = position
	for end < len(input) && IsDigit(input[end]) {
		number = number*parameterNumberBase + int(input[end]-'0')
		if number > maximum {
			return end, 0, ParseErrorAt(input, start, end, fmt.Sprintf("invalid parameter number: exceeds maximum of %d", maximum))
		}
		end++
	}
	return end, number, nil
}
