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

import (
	"go/ast"
	"go/token"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/sqlmodel"
)

// NullableOverride returns a go_type override, wrapping its name in a pointer when the
// column or parameter is nullable, so a nullable column declared with `go_type:
// github.com/google/uuid.UUID` becomes `*uuid.UUID`. A slice that holds an array stays a
// slice, as an array's default Go type does, since a nil slice is NULL.
//
// Takes override (*sqlmodel.GoType) which is the declared override, or nil when none was
// declared.
// Takes sqlType (sqlmodel.SQLType) which is the value's SQL type.
// Takes nullable (bool) which is true when the value permits NULL.
//
// Returns *sqlmodel.GoType which is the resolved override, or nil when no override was
// declared.
func NullableOverride(override *sqlmodel.GoType, sqlType sqlmodel.SQLType, nullable bool) *sqlmodel.GoType {
	if override == nil {
		return nil
	}
	if !nullable || strings.HasPrefix(override.Name, "*") {
		return override
	}
	if sqlType.Category == sqlmodel.TypeCategoryArray && strings.HasPrefix(override.Name, "[]") {
		return override
	}
	return &sqlmodel.GoType{
		Composite: override.Composite,
		Package:   override.Package,
		Name:      "*" + override.Name,
	}
}

// JSONTagNames sanitises the SQL names of one struct's fields into JSON tag names and
// suffixes any that repeat, so two columns with the same name (such as a.id and b.id in a
// join) never share a tag, which encoding/json would silently drop and go vet reports.
//
// Takes names ([]string) which are the SQL column or parameter names in field order.
//
// Returns []string which are the unique tag names in the same order.
func JSONTagNames(names []string) []string {
	sanitised := make([]string, len(names))
	for index, name := range names {
		sanitised[index] = goastutil.SanitiseJSONTagName(name)
	}
	return DisambiguateNames(sanitised)
}

// DocComment returns the comment group of a declaration's or a field's doc comment.
//
// Takes lines ([]string) which are the comment's lines, each with its marker, as
// emitkit.CommentLines writes them.
//
// Returns *ast.CommentGroup which holds the lines, or nil for none, so the declaration
// carries no doc comment.
func DocComment(lines []string) *ast.CommentGroup {
	if len(lines) == 0 {
		return nil
	}
	comments := make([]*ast.Comment, len(lines))
	for index, line := range lines {
		comments[index] = &ast.Comment{Slash: token.NoPos, Text: line}
	}
	return &ast.CommentGroup{List: comments}
}

// TagLiteral returns a field's struct tag as the raw string literal a field declares it
// with.
//
// Takes tag (string) which is the tag without back quotes, such as json:"id", which holds
// none.
//
// Returns *ast.BasicLit which holds the tag, or nil for an empty tag, so the field
// declares none.
func TagLiteral(tag string) *ast.BasicLit {
	if tag == "" {
		return nil
	}
	return &ast.BasicLit{
		Kind:  token.STRING,
		Value: "`" + tag + "`",
	}
}
