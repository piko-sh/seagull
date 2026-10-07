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

package goast

const (
	// IdentQueries represents the Queries struct name.
	IdentQueries = "Queries"

	// IdentQueriesReceiver represents the receiver variable name for Queries methods.
	IdentQueriesReceiver = "queries"

	// IdentDB represents the db parameter name.
	IdentDB = "db"

	// IdentReader represents the reader field name for read-only database connections.
	IdentReader = "reader"

	// IdentWriter represents the writer field name for read-write database connections.
	IdentWriter = "writer"

	// IdentRow represents the row local variable name.
	IdentRow = "row"

	// IdentRows represents the rows local variable name.
	IdentRows = "rows"

	// IdentRaw represents the local holding the values a row is scanned into as Go's own
	// strings before they are converted to the enum types the package declares.
	IdentRaw = "raw"

	// IdentErr represents the err variable name.
	IdentErr = "err"

	// IdentNil represents the nil literal.
	IdentNil = "nil"

	// IdentResults represents the results variable name.
	IdentResults = "results"

	// IdentError represents the error built-in type name.
	IdentError = "error"

	// IdentCtx represents the ctx parameter name.
	IdentCtx = "ctx"

	// IdentBuilder represents the builder variable name used for strings.Builder instances.
	IdentBuilder = "builder"

	// IdentQuery represents the query variable name.
	IdentQuery = "query"

	// IdentString represents the string built-in type name.
	IdentString = "string"

	// IdentInt represents the int built-in type name.
	IdentInt = "int"

	// IdentPlaceholderField is the Placeholder field name on the slice-expansion remap entry
	// referenced by the generated slice helper.
	IdentPlaceholderField = "Placeholder"

	// IdentWhereArgs represents the whereArgs variable name for dynamic WHERE clause
	// arguments.
	IdentWhereArgs = "whereArgs"

	// IdentWhereClauses represents the whereClauses variable name for dynamic WHERE clause
	// fragments.
	IdentWhereClauses = "whereClauses"

	// IdentOrderByClauses represents the orderByClauses variable name for dynamic ORDER BY
	// clause fragments.
	IdentOrderByClauses = "orderByClauses"

	// IdentArgs names the local slice of values bound by a generated query call.
	IdentArgs = "args"

	// IdentParameterCount represents the local parameterCount counter used inside
	// buildQuery() when assigning numbered placeholders to LIMIT and OFFSET.
	IdentParameterCount = "parameterCount"

	// IdentContext represents the context package name.
	IdentContext = "context"

	// IdentContextType represents the Context type name from the context package.
	IdentContextType = "Context"

	// IdentBlank represents the blank identifier.
	IdentBlank = "_"

	// IdentItems represents the items variable name.
	IdentItems = "items"

	// IdentTransaction represents the transaction variable name.
	IdentTransaction = "transaction"

	// IdentBatch represents the batch variable name.
	IdentBatch = "batch"

	// IdentParams holds the identifier name for the params argument variable.
	IdentParams = "params"

	// IdentOrderDirection holds the identifier name for the OrderDirection type.
	IdentOrderDirection = "OrderDirection"

	// IdentAny holds the identifier name for the built-in any (alias of interface{}) type,
	// used by the AST emitters for generic value holders.
	IdentAny = "any"

	// IdentBool holds the identifier name for the built-in bool type.
	IdentBool = "bool"

	// DefaultMaxBindVariablesFallback is the cap the generated code keeps to when the
	// dialect sets no limit of its own on the parameters one statement binds.
	DefaultMaxBindVariablesFallback = 65535
)
