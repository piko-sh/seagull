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

package helpers

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"seagull.sh/seagull/internal/gogen/goast"
)

const (
	// canonicalHelper starts the name of every helper a source declares, such as
	// sglMaxBindVariables.
	canonicalHelper = "sgl"

	// canonicalSentinel starts the name of every unexported error a source declares, such as
	// errSglLimit.
	canonicalSentinel = "errSgl"

	// canonicalErrorValue starts the name of every exported error a source declares, such as
	// ErrSglTooManyBindVariables.
	canonicalErrorValue = "ErrSgl"

	// canonicalMessage starts the text of every error message a source writes, such as "sgl:
	// too many bind variables".
	canonicalMessage = "sgl: "

	// quoteLength is the length of the quote that opens a string literal.
	quoteLength = 1
)

// errUnsupportedValue is returned for a data value of a type no Go literal is written
// for.
var errUnsupportedValue = errors.New("unsupported helper value")

// planNames writes every use of an inlined declaration as its value, and renames the
// canonical identifiers and message literals after the vocabulary.
//
// Returns error when an inlined value cannot be written as a Go literal.
func (r *renderer) planNames() error {
	fields := make(map[*ast.Ident]bool)
	for node := range ast.Preorder(r.file) {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			fields[typed.Sel] = true
		case *ast.Ident:
			if err := r.planIdentifier(typed, fields[typed]); err != nil {
				return err
			}
		case *ast.BasicLit:
			r.planMessage(typed)
		}
	}
	return nil
}

// planIdentifier writes an identifier as the value of the inlined declaration it names,
// or renames it.
//
// Takes identifier (*ast.Ident) which is the identifier.
// Takes field (bool) which is true when it names a field or method after a dot, which no
// inlined declaration can be.
//
// Returns error when the inlined value cannot be written as a Go literal.
func (r *renderer) planIdentifier(identifier *ast.Ident, field bool) error {
	start, end := r.offset(identifier.Pos()), r.offset(identifier.End())
	if value, inlined := r.inlined[identifier.Name]; inlined && !field {
		text, err := r.literal(value, false)
		if err != nil {
			return fmt.Errorf("inline %q: %w", identifier.Name, err)
		}
		r.add(edit{start: start, end: end, replacement: text, expand: nil})
		return nil
	}
	if renamed := r.rename(identifier.Name); renamed != identifier.Name {
		r.add(edit{start: start, end: end, replacement: renamed, expand: nil})
	}
	return nil
}

// planMessage rewrites the canonical prefix of a message literal, such as "sgl: too many
// bind variables", with the vocabulary's error prefix.
//
// Takes literal (*ast.BasicLit) which is the literal.
func (r *renderer) planMessage(literal *ast.BasicLit) {
	if literal.Kind != token.STRING || len(literal.Value) <= quoteLength+len(canonicalMessage) {
		return
	}
	if literal.Value[quoteLength:quoteLength+len(canonicalMessage)] != canonicalMessage {
		return
	}
	rewritten := literal.Value[:quoteLength] + r.vocabulary.Message("") + literal.Value[quoteLength+len(canonicalMessage):]
	r.add(edit{start: r.offset(literal.Pos()), end: r.offset(literal.End()), replacement: rewritten, expand: nil})
}

// rename returns the name an identifier of the source is rendered with: its rename when
// the data gives one, its name after the vocabulary when it is canonical, and itself
// otherwise.
//
// Takes name (string) which is the identifier.
//
// Returns string which is the rendered name.
func (r *renderer) rename(name string) string {
	if renamed, known := r.data.Renames[name]; known {
		return renamed
	}
	switch {
	case canonical(name, canonicalErrorValue):
		return r.vocabulary.ErrorValue(name[len(canonicalErrorValue):])
	case canonical(name, canonicalSentinel):
		return r.vocabulary.Sentinel(name[len(canonicalSentinel):])
	case canonical(name, canonicalHelper):
		return r.vocabulary.Helper(name[len(canonicalHelper):])
	default:
		return name
	}
}

// renameText renames every identifier a comment mentions, word by word.
//
// Takes text (string) which is the comment's text.
//
// Returns string which is the text with every word renamed.
func (r *renderer) renameText(text string) string {
	var builder strings.Builder
	word := -1
	for index, character := range text {
		inWord := character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character)
		switch {
		case inWord && word < 0:
			word = index
		case !inWord && word >= 0:
			builder.WriteString(r.rename(text[word:index]))
			word = -1
			builder.WriteString(string(character))
		case !inWord:
			builder.WriteString(string(character))
		}
	}
	if word >= 0 {
		builder.WriteString(r.rename(text[word:]))
	}
	return builder.String()
}

// literal writes a value as Go source and records the imports it needs.
//
// Takes value (any) which is a string, int, rune, bool, []string or Code.
// Takes raw (bool) which writes a string as a raw literal when it can be one.
//
// Returns string which is the Go source of the value.
// Returns error when the value's type has no Go literal.
func (r *renderer) literal(value any, raw bool) (string, error) {
	switch typed := value.(type) {
	case string:
		if raw {
			return goast.StringLiteral(typed).Value, nil
		}
		return strconv.Quote(typed), nil
	case int:
		return strconv.Itoa(typed), nil
	case rune:
		return strconv.QuoteRune(typed), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case []string:
		quoted := make([]string, len(typed))
		for index, element := range typed {
			quoted[index] = strconv.Quote(element)
		}
		return "[]string{" + strings.Join(quoted, ", ") + "}", nil
	case Code:
		r.imports = append(r.imports, typed.Imports...)
		return typed.Text, nil
	default:
		return "", fmt.Errorf("%w: %T", errUnsupportedValue, value)
	}
}

// canonical reports whether a name is a canonical prefix followed by a capitalised word,
// such as sglMaxBindVariables for the prefix sgl.
//
// Takes name (string) which is the name.
// Takes prefix (string) which is the canonical prefix.
//
// Returns bool which is true when the name starts with the prefix and a capital.
func canonical(name, prefix string) bool {
	rest, found := strings.CutPrefix(name, prefix)
	if !found {
		return false
	}
	first, size := utf8.DecodeRuneInString(rest)
	return size > 0 && unicode.IsUpper(first)
}
