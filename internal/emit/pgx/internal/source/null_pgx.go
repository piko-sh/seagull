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

//go:build ignore

package source

import (
	"database/sql/driver"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// sglMicrosecondsPerDay is the number of microseconds in the day of an interval.
	sglMicrosecondsPerDay = 24 * 60 * 60 * 1000 * 1000

	// sglMicrosecondsPerMonth is the number of microseconds in the 30-day month of an
	// interval, as pgx counts one.
	sglMicrosecondsPerMonth = 30 * sglMicrosecondsPerDay
)

// ScanText implements pgtype.TextScanner, so pgx reads text into a Null[string] without
// going through database/sql's conversion.
//
// Takes value (pgtype.Text) which is the text, or NULL.
//
// Returns error when the text cannot be stored in T.
func (n *Null[T]) ScanText(value pgtype.Text) error {
	if !value.Valid {
		*n = Null[T]{}
		return nil
	}
	if target, ok := any(&n.V).(*string); ok {
		*target, n.Valid = value.String, true
		return nil
	}
	return n.Scan(value.String)
}

// ScanInt64 implements pgtype.Int64Scanner, so pgx reads an integer into a Null of a Go
// integer type without going through database/sql's conversion.
//
// Takes value (pgtype.Int8) which is the integer, or NULL.
//
// Returns error when the integer does not fit T.
func (n *Null[T]) ScanInt64(value pgtype.Int8) error {
	if !value.Valid {
		*n = Null[T]{}
		return nil
	}
	switch target := any(&n.V).(type) {
	case *int64:
		*target = value.Int64
	case *int:
		if value.Int64 < math.MinInt || value.Int64 > math.MaxInt {
			return fmt.Errorf("sgl: %d overflows int", value.Int64)
		}
		*target = int(value.Int64)
	case *int32:
		if value.Int64 < math.MinInt32 || value.Int64 > math.MaxInt32 {
			return fmt.Errorf("sgl: %d overflows int32", value.Int64)
		}
		*target = int32(value.Int64)
	case *int16:
		if value.Int64 < math.MinInt16 || value.Int64 > math.MaxInt16 {
			return fmt.Errorf("sgl: %d overflows int16", value.Int64)
		}
		*target = int16(value.Int64)
	default:
		return n.Scan(value.Int64)
	}
	n.Valid = true
	return nil
}

// ScanFloat64 implements pgtype.Float64Scanner, so pgx reads a float into a Null of a Go
// float type without going through database/sql's conversion.
//
// Takes value (pgtype.Float8) which is the float, or NULL.
//
// Returns error when the float cannot be stored in T, or is finite and overflows a
// float32.
func (n *Null[T]) ScanFloat64(value pgtype.Float8) error {
	if !value.Valid {
		*n = Null[T]{}
		return nil
	}
	switch target := any(&n.V).(type) {
	case *float64:
		*target = value.Float64
	case *float32:
		if math.Abs(value.Float64) > math.MaxFloat32 && !math.IsInf(value.Float64, 0) {
			return fmt.Errorf("sgl: %g overflows float32", value.Float64)
		}
		*target = float32(value.Float64)
	default:
		return n.Scan(value.Float64)
	}
	n.Valid = true
	return nil
}

// ScanBool implements pgtype.BoolScanner.
//
// Takes value (pgtype.Bool) which is the boolean, or NULL.
//
// Returns error when the boolean cannot be stored in T.
func (n *Null[T]) ScanBool(value pgtype.Bool) error {
	if !value.Valid {
		*n = Null[T]{}
		return nil
	}
	if target, ok := any(&n.V).(*bool); ok {
		*target, n.Valid = value.Bool, true
		return nil
	}
	return n.Scan(value.Bool)
}

// ScanTimestamptz implements pgtype.TimestamptzScanner.
//
// Takes value (pgtype.Timestamptz) which is the time, or NULL.
//
// Returns error when the time is infinite or cannot be stored in T.
func (n *Null[T]) ScanTimestamptz(value pgtype.Timestamptz) error {
	return n.scanTime(value.Time, value.InfinityModifier, value.Valid, value)
}

// ScanTimestamp implements pgtype.TimestampScanner.
//
// Takes value (pgtype.Timestamp) which is the time, or NULL.
//
// Returns error when the time is infinite or cannot be stored in T.
func (n *Null[T]) ScanTimestamp(value pgtype.Timestamp) error {
	return n.scanTime(value.Time, value.InfinityModifier, value.Valid, value)
}

// ScanDate implements pgtype.DateScanner.
//
// Takes value (pgtype.Date) which is the date, or NULL.
//
// Returns error when the date is infinite or cannot be stored in T.
func (n *Null[T]) ScanDate(value pgtype.Date) error {
	return n.scanTime(value.Time, value.InfinityModifier, value.Valid, value)
}

// ScanUUID implements pgtype.UUIDScanner, storing the UUID's bytes through T's
// UnmarshalBinary, as a UUID type such as uuid.UUID has.
//
// Takes value (pgtype.UUID) which is the UUID, or NULL.
//
// Returns error when the UUID cannot be stored in T.
func (n *Null[T]) ScanUUID(value pgtype.UUID) error {
	if !value.Valid {
		*n = Null[T]{}
		return nil
	}
	if target, ok := any(&n.V).(interface{ UnmarshalBinary(data []byte) error }); ok {
		if err := target.UnmarshalBinary(value.Bytes[:]); err != nil {
			return fmt.Errorf("sgl: reading a UUID: %w", err)
		}
		n.Valid = true
		return nil
	}
	return n.scanValue(value)
}

// ScanNumeric implements pgtype.NumericScanner, so pgx hands a numeric to T exactly, as
// its text, rather than through a float.
//
// Takes value (pgtype.Numeric) which is the number, or NULL.
//
// Returns error when the number cannot be stored in T.
func (n *Null[T]) ScanNumeric(value pgtype.Numeric) error {
	if !value.Valid {
		*n = Null[T]{}
		return nil
	}
	return n.scanValue(value)
}

// ScanInterval implements pgtype.IntervalScanner, so pgx reads an interval into a
// Null[time.Duration] as it reads one into a *time.Duration, counting a month as 30 days
// and a day as 24 hours.
//
// Takes value (pgtype.Interval) which is the interval, or NULL.
//
// Returns error when the interval cannot be stored in T, or is longer than a
// time.Duration holds.
func (n *Null[T]) ScanInterval(value pgtype.Interval) error {
	if !value.Valid {
		*n = Null[T]{}
		return nil
	}
	if target, ok := any(&n.V).(*time.Duration); ok {
		const largest = math.MaxInt64 / int64(time.Microsecond)
		approximate := float64(value.Months)*sglMicrosecondsPerMonth + float64(value.Days)*sglMicrosecondsPerDay +
			float64(value.Microseconds)
		microseconds := int64(value.Months)*sglMicrosecondsPerMonth + int64(value.Days)*sglMicrosecondsPerDay + value.Microseconds
		if math.Abs(approximate) > float64(2*largest) || microseconds > largest || microseconds < -largest {
			return fmt.Errorf("sgl: the interval of %d months, %d days and %d microseconds overflows time.Duration",
				value.Months, value.Days, value.Microseconds)
		}
		*target, n.Valid = time.Duration(microseconds)*time.Microsecond, true
		return nil
	}
	return n.scanValue(value)
}

// IntervalValue implements pgtype.IntervalValuer, so pgx writes a Null[time.Duration] as
// the interval a time.Duration is written as, and any other value as the interval its
// text names.
//
// Returns pgtype.Interval which is the interval, or NULL.
// Returns error when the value names no interval.
func (n Null[T]) IntervalValue() (pgtype.Interval, error) {
	if !n.Valid {
		return pgtype.Interval{}, nil
	}
	if duration, ok := any(n.V).(time.Duration); ok {
		return pgtype.Interval{Microseconds: int64(duration / time.Microsecond), Valid: true}, nil
	}
	written, err := n.Value()
	if err != nil {
		return pgtype.Interval{}, err
	}
	var interval pgtype.Interval
	if err := interval.Scan(written); err != nil {
		return pgtype.Interval{}, err
	}
	return interval, nil
}

// scanTime stores a finite time in a Null[time.Time], and any other value as database/sql
// converts it.
//
// Takes at (time.Time) which is the time.
// Takes infinity (pgtype.InfinityModifier) which marks an infinite time.
// Takes valid (bool) which is false for NULL.
// Takes value (driver.Valuer) which is the pgx value, for the conversion.
//
// Returns error when the value cannot be stored in T.
func (n *Null[T]) scanTime(at time.Time, infinity pgtype.InfinityModifier, valid bool, value driver.Valuer) error {
	if !valid {
		*n = Null[T]{}
		return nil
	}
	if target, ok := any(&n.V).(*time.Time); ok && infinity == pgtype.Finite {
		*target, n.Valid = at, true
		return nil
	}
	return n.scanValue(value)
}

// scanValue stores the driver value of a pgx value as database/sql converts it.
//
// Takes value (driver.Valuer) which is the pgx value.
//
// Returns error when the value cannot be stored in T.
func (n *Null[T]) scanValue(value driver.Valuer) error {
	converted, err := value.Value()
	if err != nil {
		return err
	}
	return n.Scan(converted)
}
