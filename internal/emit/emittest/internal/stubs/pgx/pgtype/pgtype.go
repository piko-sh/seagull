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

// Package pgtype stands in for github.com/jackc/pgx/v5/pgtype in the runtime lane of
// emittest. It declares only the values the generated Null type's pgx scanner methods
// take.
package pgtype

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// InfinityModifier marks a time or date as finite or infinite.
type InfinityModifier int8

// Finite marks a finite time or date.
const Finite InfinityModifier = 0

// Text is a text value, or NULL.
type Text struct {
	// String is the text.
	String string

	// Valid is false for NULL.
	Valid bool
}

// Int8 is an integer, or NULL.
type Int8 struct {
	// Int64 is the integer.
	Int64 int64

	// Valid is false for NULL.
	Valid bool
}

// Float8 is a float, or NULL.
type Float8 struct {
	// Float64 is the float.
	Float64 float64

	// Valid is false for NULL.
	Valid bool
}

// Bool is a boolean, or NULL.
type Bool struct {
	// Bool is the boolean.
	Bool bool

	// Valid is false for NULL.
	Valid bool
}

// Timestamptz is a time with a time zone, or NULL.
type Timestamptz struct {
	// Time is the time.
	Time time.Time

	// InfinityModifier marks an infinite time.
	InfinityModifier InfinityModifier

	// Valid is false for NULL.
	Valid bool
}

// Value returns the time.
//
// Returns driver.Value which is the time, or nil for NULL.
// Returns error which is always nil.
func (value Timestamptz) Value() (driver.Value, error) {
	if !value.Valid {
		return nil, nil
	}
	return value.Time, nil
}

// Timestamp is a time without a time zone, or NULL.
type Timestamp struct {
	// Time is the time.
	Time time.Time

	// InfinityModifier marks an infinite time.
	InfinityModifier InfinityModifier

	// Valid is false for NULL.
	Valid bool
}

// Value returns the time.
//
// Returns driver.Value which is the time, or nil for NULL.
// Returns error which is always nil.
func (value Timestamp) Value() (driver.Value, error) {
	if !value.Valid {
		return nil, nil
	}
	return value.Time, nil
}

// Date is a date, or NULL.
type Date struct {
	// Time is the date.
	Time time.Time

	// InfinityModifier marks an infinite date.
	InfinityModifier InfinityModifier

	// Valid is false for NULL.
	Valid bool
}

// Value returns the date.
//
// Returns driver.Value which is the date, or nil for NULL.
// Returns error which is always nil.
func (value Date) Value() (driver.Value, error) {
	if !value.Valid {
		return nil, nil
	}
	return value.Time, nil
}

// UUID is a UUID, or NULL.
type UUID struct {
	// Bytes is the UUID.
	Bytes [16]byte

	// Valid is false for NULL.
	Valid bool
}

// Value returns the UUID's bytes.
//
// Returns driver.Value which is the bytes, or nil for NULL.
// Returns error which is always nil.
func (value UUID) Value() (driver.Value, error) {
	if !value.Valid {
		return nil, nil
	}
	return value.Bytes[:], nil
}

// Numeric is a number, or NULL, held here as its text.
type Numeric struct {
	// Text is the number's text.
	Text string

	// Valid is false for NULL.
	Valid bool
}

// Value returns the number's text.
//
// Returns driver.Value which is the text, or nil for NULL.
// Returns error which is always nil.
func (value Numeric) Value() (driver.Value, error) {
	if !value.Valid {
		return nil, nil
	}
	return value.Text, nil
}

// Interval is an interval, or NULL.
type Interval struct {
	// Microseconds is the interval's time part.
	Microseconds int64

	// Days is the interval's days.
	Days int32

	// Months is the interval's months.
	Months int32

	// Valid is false for NULL.
	Valid bool
}

// Value returns the interval's microseconds.
//
// Returns driver.Value which is the microseconds, or nil for NULL.
// Returns error which is always nil.
func (value Interval) Value() (driver.Value, error) {
	if !value.Valid {
		return nil, nil
	}
	return value.Microseconds, nil
}

// Scan reads an interval from its microseconds.
//
// Takes source (any) which is the microseconds, or nil for NULL.
//
// Returns error when source is neither nil nor an int64.
func (value *Interval) Scan(source any) error {
	if source == nil {
		*value = Interval{Microseconds: 0, Days: 0, Months: 0, Valid: false}
		return nil
	}
	microseconds, ok := source.(int64)
	if !ok {
		return fmt.Errorf("pgtype stand-in: cannot scan %T into an Interval", source)
	}
	*value = Interval{Microseconds: microseconds, Days: 0, Months: 0, Valid: true}
	return nil
}
