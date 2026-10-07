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

package gogen

// ColumnReading says how the generated code reads an output column.
type ColumnReading uint8

const (
	// ColumnReadAsIs scans a column as the driver returns it.
	ColumnReadAsIs ColumnReading = iota

	// ColumnReadAsJSON wraps a column in the dialect's to-JSON function and decodes the JSON
	// into the column's Go type, for a driver that cannot scan the column's value, such as
	// an array, into a Go slice, map or struct.
	ColumnReadAsJSON

	// ColumnReadAsText casts a column to the dialect's text type and scans the text, for a
	// driver that returns the column's value in a type of its own that the column's Go type
	// cannot scan, such as a decimal or an interval.
	ColumnReadAsText

	// ColumnReadConverted scans a column through the driver's sql.Scanner adapter to convert
	// it into its Go type. The driver implements ColumnConverter.
	ColumnReadConverted

	// ColumnReadRaw scans into a driver-native local and converts it after scanning.
	ColumnReadRaw

	// ColumnReadAdapted reads a column through the driver's converting scan adapter.
	//
	// Column-specific Go overrides use OverrideReader. Type-level overrides remain eligible
	// for this adapter.
	ColumnReadAdapted
)
