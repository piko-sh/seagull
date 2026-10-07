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

package sqlmodel

// ParameterDirectiveKind identifies the role of a parameter or sortable input declared in
// a query header.
//
// Cardinality (slice) and presence (optional) are represented by IsSlice and IsOptional.
// LIMIT and OFFSET are inferred from clause position. The zero value marks the absence of
// a parameter directive.
type ParameterDirectiveKind uint8

const (
	// ParameterDirectiveNone marks parameters inferred without a sgl.param declaration and
	// directive specs whose roles declare no parameter.
	ParameterDirectiveNone ParameterDirectiveKind = iota

	// ParameterDirectiveParam defines a standard query parameter.
	ParameterDirectiveParam

	// ParameterDirectiveSortable defines a sortable input (dynamic ORDER BY) declared with
	// the standalone sgl.sortable directive; it does not bind a placeholder.
	ParameterDirectiveSortable
)
