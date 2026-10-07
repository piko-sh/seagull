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

// CommentStyle describes the comment syntax used by an engine's query files. The default
// for SQL dialects is "--" line comments.
type CommentStyle struct {
	// LinePrefix is the single-line comment prefix (e.g. "--", "#", "//").
	LinePrefix string
}

// ParsedStatementRaw is a marker interface for engine-native AST nodes stored in
// ParsedStatement.Raw.
//
// Each dialect defines its own concrete type that satisfies ParsedStatementRaw. The
// analyser never inspects the contents; it passes statements back to the same dialect for
// DDL application or query analysis.
type ParsedStatementRaw interface {
	// IsParsedStatement is a marker method that distinguishes parsed statement types from
	// other interfaces.
	IsParsedStatement()
}

// ParsedStatement is an opaque wrapper around an engine-native AST node. The analyser
// never inspects the contents: it passes statements back to the dialect for DDL
// application or query analysis.
type ParsedStatement struct {
	// Raw holds the engine-native AST node, whose concrete type depends on the dialect (e.g.
	// *parsedStatement for SQLite).
	Raw ParsedStatementRaw

	// Location is the byte offset in the source SQL.
	Location int

	// Length is the byte length of the statement in the source SQL.
	Length int
}

// DefaultSQLCommentStyle returns the standard SQL comment style using "--".
//
// Returns CommentStyle which uses "--" as the line prefix.
func DefaultSQLCommentStyle() CommentStyle {
	return CommentStyle{LinePrefix: "--"}
}
