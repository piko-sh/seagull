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

package pgext

import (
	"seagull.sh/seagull/internal/dialect/ast"
)

// StatementExtensionPort lets a PostgreSQL flavour recognise and parse statements
// PostgreSQL does not have. Each extension owns one or more StatementKind values in the
// reserved range.
//
// The dialect consults extensions in registration order after its built-in classifier;
// the first extension that claims a statement parses it.
type StatementExtensionPort interface {
	// Classify inspects a candidate statement's tokens and returns a StatementKind in the
	// extension's reserved range to claim it, or 0 to decline.
	//
	// The slice is the extension's own copy, so changing it changes nothing the dialect or
	// another extension reads.
	//
	// Takes tokens ([]Token) which are the statement's tokens.
	//
	// Returns StatementKind which is the claimed kind, or 0 when the extension declines.
	Classify(tokens []Token) StatementKind

	// Parse runs after classification, driving the parser context to consume the statement
	// and producing a CatalogueMutation.
	//
	// The kind is the value Classify returned, so one extension can serve several statement
	// shapes.
	//
	// Takes p (ParserContext) which drives token consumption for the statement.
	// Takes kind (StatementKind) which is the value Classify returned.
	//
	// Returns *ast.CatalogueMutation which is the parsed catalogue change.
	// Returns error when the statement cannot be parsed.
	Parse(p ParserContext, kind StatementKind) (*ast.CatalogueMutation, error)
}
