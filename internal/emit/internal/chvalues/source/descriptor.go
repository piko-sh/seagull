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

package source

// ShapeDescription is a checked shape for generation. This file is not embedded in
// generated packages; it lets the generator use the runtime grammar without copying it.
type ShapeDescription struct {
	// Names holds tuple element names in descriptor order.
	Names []string

	// Elements holds the descriptors for nested values.
	Elements []ShapeDescription

	// Digits is the timestamp precision, or -1 when unspecified.
	Digits int

	// Kind is the shape grammar's type marker.
	Kind byte

	// Checked reports whether nested values require native conversion checks.
	Checked bool

	// DriverMap reports whether maps require the driver's native Go forms.
	DriverMap bool
}

// DescribeShape parses a shape at generation time and precomputes its native flags.
//
// Takes text (string) which is the complete encoded shape.
//
// Returns ShapeDescription which is the parsed descriptor with its native conversion
// flags.
// Returns error when the shape cannot be parsed.
func DescribeShape(text string) (ShapeDescription, error) {
	shape, err := sglClickHouseParseShape(text)
	if err != nil {
		return ShapeDescription{}, err
	}
	return describeShape(shape), nil
}

// describeShape copies a runtime descriptor into its generation form.
//
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns ShapeDescription which contains the descriptor and recursively described
// elements.
func describeShape(shape sglClickHouseShape) ShapeDescription {
	description := ShapeDescription{
		Names: shape.names, Elements: nil, Digits: shape.digits, Kind: shape.kind,
		Checked:   sglAppendNativeHolds(shape, sglAppendNativeChecked),
		DriverMap: sglAppendNativeHolds(shape, sglAppendNativeDriverMap),
	}
	for _, element := range shape.elements {
		description.Elements = append(description.Elements, describeShape(element))
	}
	return description
}
