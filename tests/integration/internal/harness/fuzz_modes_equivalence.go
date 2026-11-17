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

package harness

import (
	"math/big"
	"strconv"
	"strings"
	"time"
)

const (
	// decimalRadix is the base of interval and duration digits.
	decimalRadix = 10

	// intervalFields bounds an interval of days and a time of day.
	intervalFields = 3

	// intervalDay is the length of one interval day.
	intervalDay = 24 * time.Hour

	// intervalRounding rounds fractional seconds to the nearest nanosecond.
	intervalRounding = 0.5

	// intervalClockParts is how many fields a time of day holds, hours, minutes and seconds.
	intervalClockParts = 3
)

// intervalDayUnits are the day words of PostgreSQL's interval output.
var intervalDayUnits = map[string]bool{"day": true, "days": true}

// representationFree rewrites the canonical values whose form the type mapping lets an
// emitter choose, so that modes compare by value.
//
// An interval is a string on database/sql and a time.Duration on pgx, as the type mapping
// documents, so PostgreSQL's interval text becomes its duration. pgx v5.10.0 decodes a
// binary numeric zero without its display scale, so 0.000000 reads as 0 there and as
// 0.000000 on database/sql. That driver exclusion writes every decimal zero alike.
//
// Takes rows ([][]string) which holds canonical rows.
//
// Returns [][]string which holds the rewritten rows.
func representationFree(rows [][]string) [][]string {
	rewritten := make([][]string, len(rows))
	for index, row := range rows {
		rewritten[index] = make([]string, len(row))
		for position, value := range row {
			rewritten[index][position] = representationFreeValue(value)
		}
	}
	return rewritten
}

// representationFreeValue rewrites one canonical value.
//
// Takes value (string) which is a canonical value.
//
// Returns string which is the value's emitter-independent form.
func representationFreeValue(value string) string {
	quoted, isText := strings.CutPrefix(value, "text:")
	if !isText {
		return value
	}
	text, err := strconv.Unquote(quoted)
	if err != nil {
		return value
	}
	if duration, ok := intervalDuration(text); ok {
		return "duration:" + strconv.FormatInt(int64(duration), decimalRadix)
	}
	if number, ok := new(big.Rat).SetString(text); ok && number.Sign() == 0 && strings.Trim(text, "0.-") == "" {
		return `text:"0"`
	}
	return value
}

// intervalDuration parses PostgreSQL's default interval output when it holds only days
// and a time of day, such as "-28 days", "1 day 02:00:00" or "-01:30:00.5".
//
// Takes text (string) which may be an interval's text.
//
// Returns time.Duration which is the interval's length.
// Returns bool which is false for any other text, including months and years.
func intervalDuration(text string) (time.Duration, bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 || len(fields) > intervalFields {
		return 0, false
	}
	var total time.Duration
	matched := false
	for index := 0; index < len(fields); index++ {
		if index+1 < len(fields) && intervalDayUnits[fields[index+1]] {
			days, err := strconv.ParseInt(fields[index], decimalRadix, 32)
			if err != nil {
				return 0, false
			}
			total += time.Duration(days) * intervalDay
			index++
			matched = true
			continue
		}
		clock, ok := intervalClock(fields[index])
		if !ok {
			return 0, false
		}
		total += clock
		matched = true
	}
	return total, matched
}

// intervalClock parses a signed HH:MM:SS time with an optional fraction.
//
// Takes field (string) which may be a time of day.
//
// Returns time.Duration which is the time's length.
// Returns bool which is false for any other text.
func intervalClock(field string) (time.Duration, bool) {
	negative := strings.HasPrefix(field, "-")
	parts := strings.Split(strings.TrimLeft(field, "+-"), ":")
	if len(parts) != intervalClockParts {
		return 0, false
	}
	hours, hoursErr := strconv.ParseUint(parts[0], decimalRadix, 32)
	minutes, minutesErr := strconv.ParseUint(parts[1], decimalRadix, 8)
	seconds, secondsErr := strconv.ParseFloat(parts[2], 64)
	if hoursErr != nil || minutesErr != nil || secondsErr != nil {
		return 0, false
	}
	whole := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds*float64(time.Second)+intervalRounding)
	if negative {
		whole = -whole
	}
	return whole, true
}
