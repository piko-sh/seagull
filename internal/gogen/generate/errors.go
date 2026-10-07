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

package generate

import "errors"

var (
	// ErrInvalidRequest is wrapped by errors for requests that cannot be emitted as they
	// are.
	//
	// These include nil requests or queries; package names, headers, helper prefixes or
	// error prefixes that generated code cannot carry; naming plans that do not fit the
	// queries, catalogue or named types; and names that cannot be planned.
	ErrInvalidRequest = errors.New("invalid emit request")

	// errNoMethod reports a query the naming plan names no method for.
	errNoMethod = errors.New("the naming plan names no method for the query")

	// errNotIdentifier reports a query name the naming plan gives that is not a Go
	// identifier.
	errNotIdentifier = errors.New("the naming plan gives a name that is not a Go identifier")

	// errNotParallel reports a slice of the naming plan that is not as long as what it runs
	// parallel to.
	errNotParallel = errors.New("the naming plan does not fit the query")
)
