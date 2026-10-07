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

package dialect

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Facts is what generated code, and the tools that run the generator, read about a
// dialect: its name and family, how it writes parameters, identifiers, lists, row limits
// and the predicates a runtime filter appends, the drivers it runs on and the statements
// that return rows.
//
// A Dialect's Info returns one, and an emitter reads one from its request. Facts belong
// to the caller, which may keep or change them: each is built afresh and shares no slice
// with the dialect.
type Facts struct {
	// Placeholders describes how the dialect writes query parameters: the forms a query or a
	// directive line may write one in, and the form the generated code executes.
	Placeholders Placeholders

	// IdentifierQuote is how the dialect delimits an identifier, which the generated code
	// writes when it names a column itself.
	IdentifierQuote IdentifierQuote

	// Name identifies the dialect, such as postgres or cockroachdb. A dialect built with a
	// custom name reports that name here.
	Name string

	// Family names the base dialect whose syntax this dialect shares, such as postgres.
	// Tools use it to select shared behaviour.
	Family string

	// CommentPrefix opens a line comment in the dialect's SQL, such as --, which the
	// generated code writes before a comment it adds to a query.
	CommentPrefix string

	// TextType names the SQL type used to cast otherwise unreadable values to text. Empty
	// means the dialect provides no such type.
	TextType string

	// ToJSON names a function used to convert otherwise unreadable column values to JSON for
	// decoding. It is empty when the dialect provides none.
	ToJSON string

	// Filters describes what the generated runtime query builder may write into a predicate
	// it appends: the dialect's extra comparison operators, the functions that read a path
	// from a JSON column, and the conversions a filtered column may take.
	Filters FilterSyntax

	// Lists describes membership tests for caller-supplied lists, including empty IN and NOT
	// IN tests and optional array forms.
	Lists ListSyntax

	// Pagination describes how the dialect writes the row limit and offset the generated
	// runtime query builder appends.
	Pagination PaginationSyntax

	// JSONOperators lists the dialect's operators that read a path from a JSON value, such
	// as -> and ->>, in sorted order, or nothing when it has none. A filtered column may be
	// written with one of them.
	JSONOperators []string

	// Drivers lists supported Go driver module paths in preference order. Emitters use the
	// first recognised driver unless one is explicitly selected.
	Drivers []string

	// Lexical defines the dialect's SQL quoting, comments, placeholders and statement
	// boundaries. The zero value uses standard SQL rules.
	Lexical sqllex.Rules

	// MaxParameters is the most parameters one statement may bind, which the generated code
	// respects when it expands a slice or batches rows. Zero means the engine sets no limit,
	// and the generated code keeps to a conservative default.
	MaxParameters int

	// Identifiers is how the dialect treats the letter case of identifiers, which a schema
	// catalogue built for it records.
	Identifiers sqlmodel.IdentifierCase

	// Returning lists statement kinds that support RETURNING. An empty set means none are
	// supported.
	Returning ast.DataChange
}

// Facts returns the facts of the dialect the Info describes, sharing no slice with it.
//
// Returns Facts which holds them, or the zero Facts for a nil receiver.
func (info *Info) Facts() Facts {
	if info == nil {
		return Facts{}
	}
	clone := info.Clone()
	return Facts{
		Placeholders:    clone.Placeholders,
		IdentifierQuote: clone.IdentifierQuote,
		Name:            clone.Name,
		Family:          clone.Family,
		CommentPrefix:   clone.Comments.LinePrefix,
		TextType:        clone.TextType,
		ToJSON:          clone.ToJSON,
		Filters:         clone.Filters,
		Lists:           clone.Lists,
		Pagination:      clone.Pagination,
		JSONOperators:   JSONOperators(clone.OperatorFeatures),
		Drivers:         clone.Drivers,
		Lexical:         clone.Lexical,
		MaxParameters:   clone.MaxParameters,
		Identifiers:     clone.Identifiers,
		Returning:       clone.Returning,
	}
}
