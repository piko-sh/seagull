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

package directive

// QueryDirectives holds the query-level settings a query header's metadata directives
// declare, which the analyser folds into the analysed query.
type QueryDirectives struct {
	// NullableOverride forces nullability on or off for the entire query result.
	NullableOverride *bool

	// Description is the prose of the query's header, as DirectiveBlock.Description holds
	// it.
	Description string

	// ReadOnlyOverride forces the query's read-only flag on or off, overriding the
	// automatically detected value from statement analysis.
	ReadOnlyOverride *bool

	// Model is the table, as the model: keyword argument writes it, optionally
	// schema-qualified, whose model the query's rows are read into, or empty when the query
	// reads them into a struct of its own.
	Model string

	// Row is the name the row: keyword argument gives the struct the query's rows are read
	// into, which every query naming it shares, or empty when it gives none.
	Row string

	// GroupByKeys holds column references from the query header's group_by keyword.
	GroupByKeys []string

	// DynamicRuntime indicates the query header set dynamic: runtime, causing generation of
	// a fluent runtime query builder.
	DynamicRuntime bool

	// Optional indicates a sgl.query optional: true directive, causing a command:one query
	// to return (row, false, nil) when no row matches instead of the driver no-rows
	// sentinel.
	Optional bool
}
