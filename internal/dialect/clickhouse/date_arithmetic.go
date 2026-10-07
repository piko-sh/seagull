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

package clickhouse

import (
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// engineNameDate is ClickHouse's Date, a day since 1970.
	engineNameDate = "Date"

	// engineNameDate32 is ClickHouse's Date32, a day over a wider range.
	engineNameDate32 = "Date32"

	// engineNameDateTime is ClickHouse's DateTime, a second since 1970.
	engineNameDateTime = "DateTime"

	// engineNameDateTime64 is ClickHouse's DateTime64, a moment at a sub-second precision.
	engineNameDateTime64 = "DateTime64"

	// date32ClockPrecision is the precision of the DateTime64 a Date32 moved by a time of
	// day becomes.
	date32ClockPrecision = 3

	// dateArgumentPosition is the position of the date or time among the arguments of
	// date_add and its spellings, after the unit and the amount.
	dateArgumentPosition = 2
)

// dateUnit is a unit a date or time is moved by.
type dateUnit struct {
	// precision is the number of decimal places of a second the unit needs, three for a
	// millisecond; zero for a second or a coarser unit.
	precision int

	// clock reports a unit of the time of day, an hour or finer, which moves a date to a
	// moment within the day.
	clock bool
}

var (
	// dateUnits maps the name of each interval unit, as an Interval type names it, to the
	// unit.
	dateUnits = map[string]dateUnit{
		"Nanosecond":  {precision: 9, clock: true},
		"Microsecond": {precision: 6, clock: true},
		"Millisecond": {precision: 3, clock: true},
		"Second":      {precision: 0, clock: true},
		"Minute":      {precision: 0, clock: true},
		"Hour":        {precision: 0, clock: true},
		"Day":         {precision: 0, clock: false},
		"Week":        {precision: 0, clock: false},
		"Month":       {precision: 0, clock: false},
		"Quarter":     {precision: 0, clock: false},
		"Year":        {precision: 0, clock: false},
	}

	// unitArgumentAdders names the functions that move a date or time by an amount of the
	// unit their first argument names, as date_add(HOUR, 1, ts), keyed in lower case.
	unitArgumentAdders = map[string]bool{
		"dateadd": true, "date_add": true, "timestampadd": true, "timestamp_add": true,
		"datesub": true, "date_sub": true, "timestampsub": true, "timestamp_sub": true,
	}
)

// movedPointInTime types a date or time shifted by a given unit.
//
// Calendar units preserve the type. Time units promote dates to timestamps; fractional
// units preserve or increase timestamp precision and reject dates.
//
// Takes point (sqlmodel.SQLType) which is the date or time.
// Takes unit (dateUnit) which is the unit it is moved by.
//
// Returns sqlmodel.SQLType which is the moved value's type, unknown where ClickHouse
// refuses the move.
func movedPointInTime(point sqlmodel.SQLType, unit dateUnit) sqlmodel.SQLType {
	switch {
	case !unit.clock:
		return point
	case point.EngineName == engineNameDate && unit.precision == 0:
		return primitiveType(engineNameDateTime)
	case point.EngineName == engineNameDate32 && unit.precision == 0:
		return dateTime64Type(date32ClockPrecision)
	case point.Temporal == sqlmodel.TemporalKindDate:
		return unknownType()
	case point.Precision != nil:
		return dateTime64Type(max(*point.Precision, unit.precision))
	case point.EngineName == engineNameDateTime && unit.precision > 0:
		return dateTime64Type(unit.precision)
	default:
		return point
	}
}

// movedByValue types a date or time moved by a value: by the unit of an interval, and by
// a number of the point's own units, which keeps its type.
//
// Takes point (sqlmodel.SQLType) which is the date or time.
// Takes mover (sqlmodel.SQLType) which is the interval or the number.
//
// Returns sqlmodel.SQLType which is the moved value's type.
func movedByValue(point sqlmodel.SQLType, mover sqlmodel.SQLType) sqlmodel.SQLType {
	if unit, isInterval := intervalUnit(mover); isInterval {
		return movedPointInTime(point, unit)
	}
	return point
}

// intervalUnit returns the unit of an Interval type.
//
// Takes interval (sqlmodel.SQLType) which is the type.
//
// Returns dateUnit which is the unit.
// Returns bool which is true when the type is an interval of a known unit.
func intervalUnit(interval sqlmodel.SQLType) (dateUnit, bool) {
	name, isInterval := strings.CutPrefix(interval.EngineName, intervalTypePrefix)
	unit, known := dateUnits[name]
	return unit, isInterval && known && interval.Temporal == sqlmodel.TemporalKindInterval
}

// dateTime64Type returns a DateTime64 of a precision.
//
// Takes precision (int) which is the number of decimal places of a second.
//
// Returns sqlmodel.SQLType which is the type.
func dateTime64Type(precision int) sqlmodel.SQLType {
	return normaliseBuiltinTypeName(engineNameDateTime64 + "(" + strconv.Itoa(precision) + ")")
}

// namedUnitAdders returns the resolvers of the functions that name the unit they move a
// date or time by, addDays to subtractNanoseconds, keyed in lower case.
//
// Returns map[string]baseResolverFunc which holds the resolvers.
func namedUnitAdders() map[string]baseResolverFunc {
	resolvers := make(map[string]baseResolverFunc, 2*len(dateUnits))
	for name, unit := range dateUnits {
		resolver := ignoreName(func(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
			return resolveMovedDate(argumentTypes, 0, unit)
		})
		plural := strings.ToLower(name) + "s"
		resolvers["add"+plural] = resolver
		resolvers["subtract"+plural] = resolver
	}
	return resolvers
}

// resolveUnitArgumentAdder types date_add and its spellings, whose first argument is the
// unit, read by the parser as an interval of that unit, and whose third is the date or
// time it moves.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns *ast.FunctionResolution which types the moved value, or nil when the call does
// not name a unit the parser read.
func resolveUnitArgumentAdder(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) <= dateArgumentPosition {
		return nil
	}
	unit, isInterval := intervalUnit(argumentTypes[0])
	if !isInterval {
		return nil
	}
	return resolveMovedDate(argumentTypes, dateArgumentPosition, unit)
}

// resolveMovedDate types a call that moves the date or time at a position by a unit, NULL
// when an argument is.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
// Takes position (int) which is the position of the date or time.
// Takes unit (dateUnit) which is the unit.
//
// Returns *ast.FunctionResolution which types the moved value, or nil when the call has
// no argument at the position.
func resolveMovedDate(argumentTypes []sqlmodel.SQLType, position int, unit dateUnit) *ast.FunctionResolution {
	if position >= len(argumentTypes) {
		return nil
	}
	point := argumentTypes[position]
	point.Nullable = false
	return newReadOnlyResolution(movedPointInTime(point, unit))
}
