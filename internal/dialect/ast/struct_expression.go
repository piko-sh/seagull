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

package ast

// StructExpression builds a struct value from named fields, such as DuckDB's {'a': 1} or
// struct_pack(a := 1). The struct has one field for each name, in order, holding the
// value at the same position.
type StructExpression struct {
	// Names holds the field names in order, parallel to Values.
	Names []string

	// Values holds the field values in order, parallel to Names.
	Values []Expression

	// Span locates the constructor in the parsed SQL. Zero means the location was not
	// recorded.
	Span Span
}

// expressionKind identifies this expression as a struct constructor.
//
// Returns string which holds the expression kind tag.
func (*StructExpression) expressionKind() string { return "struct" }
