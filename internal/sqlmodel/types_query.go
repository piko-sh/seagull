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

// ParameterContext describes where a parameter appears in a query.
type ParameterContext uint8

const (
	// ParameterContextComparison indicates a parameter in a comparison expression.
	ParameterContextComparison ParameterContext = iota

	// ParameterContextAssignment indicates a parameter in a SET assignment.
	ParameterContextAssignment

	// ParameterContextFunctionArgument indicates a parameter as a function argument.
	ParameterContextFunctionArgument

	// ParameterContextCast indicates a parameter inside a CAST expression.
	ParameterContextCast

	// ParameterContextInList indicates a parameter in an IN list.
	ParameterContextInList

	// ParameterContextBetween indicates a parameter in a BETWEEN expression.
	ParameterContextBetween

	// ParameterContextLimit indicates a parameter in a LIMIT clause.
	ParameterContextLimit

	// ParameterContextOffset indicates a parameter in an OFFSET clause.
	ParameterContextOffset

	// ParameterContextLike indicates a parameter inside a LIKE / ILIKE / GLOB / REGEXP
	// pattern expression. The associated ColumnReference (when set) names the column on the
	// left of the pattern operator; the parameter itself always carries a string pattern, so
	// the analyser types it as text regardless of the column's type.
	ParameterContextLike

	// ParameterContextUnknown indicates a parameter in an unrecognised context.
	ParameterContextUnknown
)
