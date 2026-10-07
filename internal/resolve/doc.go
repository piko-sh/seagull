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

// Package resolve types the expressions of an analysed query against the schema
// catalogue.
//
// ScopeChain models the nested scopes of a query (CTEs, subqueries, LATERAL joins and
// function parameters) and resolves column references through them; the depth of a scope
// is the nesting depth of its query, one budget of MaxExpressionResolveDepth levels that
// every walk into a nested query shares. ScopeBuilder populates a scope chain from a raw
// query analysis. TypeResolver infers the SQL type and nullability of every output column
// and parameter, using the function resolver for overloaded calls and the dialect's type
// rules for promotion and implicit casts. None of them is safe for concurrent use.
package resolve
