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
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PlaceholderKind says what follows a placeholder's prefix.
type PlaceholderKind uint8

const (
	// PlaceholderNumbered is a placeholder followed by the parameter's number, counting from
	// one, such as $1. A parameter written twice is bound once.
	PlaceholderNumbered PlaceholderKind = iota

	// PlaceholderAnonymous is a placeholder that stands alone, such as ?.
	//
	// Each one binds the next argument in order, so a parameter written twice is bound
	// twice. A directive line still anchors one by number, counting the placeholders in the
	// order they appear.
	PlaceholderAnonymous

	// PlaceholderNamed is a placeholder followed by the parameter's name, such as :name.
	PlaceholderNamed
)

// PlaceholderSigil is one way of writing a query parameter: the text that opens it, what
// follows, and the text that closes it, if any.
type PlaceholderSigil struct {
	// Prefix opens the placeholder, such as "$", "?", ":", "@p" or "{".
	Prefix string

	// Suffix closes the placeholder, such as "}" for {name:Type}, or is empty when the
	// placeholder ends where its number or name does.
	Suffix string

	// TypeSeparator, for a named placeholder that carries the parameter's type, separates
	// the name from the type, such as ":" in {name:Type}. It is empty for a placeholder that
	// carries no type.
	TypeSeparator string

	// Kind says what follows the prefix.
	Kind PlaceholderKind
}

// Render writes one placeholder in the sigil's form: the prefix, then the parameter's
// number, nothing or its name as the kind says, the type after the type separator when
// the sigil carries one, and the suffix.
//
// Takes number (int) which is the parameter's number, counting from one.
// Takes name (string) which is the parameter's name as bound.
// Takes typeName (string) which is the parameter's type as the engine spells it, written
// only when the sigil carries a type.
//
// Returns string which is the placeholder.
func (sigil PlaceholderSigil) Render(number int, name string, typeName string) string {
	var builder strings.Builder
	builder.WriteString(sigil.Prefix)
	switch sigil.Kind {
	case PlaceholderNumbered:
		builder.WriteString(strconv.Itoa(number))
	case PlaceholderNamed:
		builder.WriteString(name)
		if sigil.TypeSeparator != "" {
			builder.WriteString(sigil.TypeSeparator)
			builder.WriteString(typeName)
		}
	case PlaceholderAnonymous:
	}
	builder.WriteString(sigil.Suffix)
	return builder.String()
}

// read reads a placeholder in this form.
//
// Takes text (string) which is the placeholder exactly as written.
//
// Returns PlaceholderReading which says what the placeholder holds.
// Returns bool which is false when the text is not written in this form.
func (sigil PlaceholderSigil) read(text string) (PlaceholderReading, bool) {
	reading := PlaceholderReading{Name: "", Type: "", Sigil: sigil, TypeOffset: 0, Number: 0}
	if sigil.Prefix == "" || len(text) < len(sigil.Prefix)+len(sigil.Suffix) ||
		!strings.HasPrefix(text, sigil.Prefix) || !strings.HasSuffix(text, sigil.Suffix) {
		return reading, false
	}
	body := text[len(sigil.Prefix) : len(text)-len(sigil.Suffix)]
	switch sigil.Kind {
	case PlaceholderNumbered:
		if body == "" {
			return reading, true
		}
		number, err := strconv.Atoi(body)
		if err != nil || number < 1 || strings.TrimLeft(body, "0123456789") != "" {
			return reading, false
		}
		reading.Number = number
		return reading, true
	case PlaceholderAnonymous:
		return reading, body == ""
	case PlaceholderNamed:
		return sigil.readNamed(reading, body)
	default:
		return reading, false
	}
}

// readNamed reads the name, and the type when the form carries one, of a named
// placeholder.
//
// Takes reading (PlaceholderReading) which is the reading so far.
// Takes body (string) which is the placeholder between its prefix and suffix.
//
// Returns PlaceholderReading which holds the name and type.
// Returns bool which is false when the body is not a name, or lacks the type the form
// carries.
func (sigil PlaceholderSigil) readNamed(reading PlaceholderReading, body string) (PlaceholderReading, bool) {
	name := body
	if sigil.TypeSeparator != "" {
		before, after, ok := strings.Cut(body, sigil.TypeSeparator)
		if !ok || strings.TrimSpace(after) == "" {
			return reading, false
		}
		name = before
		reading.Type = after
		reading.TypeOffset = len(sigil.Prefix) + len(before) + len(sigil.TypeSeparator)
	}
	if !isPlaceholderName(name) {
		return reading, false
	}
	reading.Name = name
	return reading, true
}

// Placeholders describes how a dialect writes the parameters of its queries: the forms a
// query or a directive line may write one in, and the form the SQL the generated code
// executes writes each one in.
//
// Code generation renders every placeholder it writes from this description, so a new
// engine's parameter syntax needs no change outside its dialect.
type Placeholders struct {
	// NamePrefix prefixes the name of every parameter the generated code binds by name, in
	// the placeholder and in the binding alike, so a parameter named after a word the engine
	// reserves still reaches it. It is empty when names are bound as written.
	NamePrefix string

	// Execution is the form the SQL the generated code executes writes each parameter in:
	// numbered, anonymous or named, as the engine binds them. Named parameters a query
	// writes in another form are rewritten to it.
	Execution PlaceholderSigil

	// Positional supplies a positional bind form when Execution uses named placeholders.
	// Zero uses Execution's form instead.
	Positional PlaceholderSigil

	// Sigils lists the forms a query, or the anchor of a directive line, may write a
	// parameter in.
	Sigils []PlaceholderSigil
}

// BoundName returns the name the generated code binds a named parameter under: the
// parameter's name after NamePrefix.
//
// Takes name (string) which is the parameter's name.
//
// Returns string which is the bound name.
func (placeholders Placeholders) BoundName(name string) string {
	return placeholders.NamePrefix + name
}

// PositionalForm returns the numbered or anonymous form the generated code writes where
// it binds by position: Positional when it is set, and Execution otherwise.
//
// Returns PlaceholderSigil which is the positional form.
func (placeholders Placeholders) PositionalForm() PlaceholderSigil {
	if placeholders.Positional.Prefix != "" {
		return placeholders.Positional
	}
	return placeholders.Execution
}

// BindsByName reports whether the SQL the generated code executes binds its parameters by
// name rather than by position.
//
// Returns bool which is true when Execution is a named form.
func (placeholders Placeholders) BindsByName() bool {
	return placeholders.Execution.Kind == PlaceholderNamed
}

// Read parses a placeholder using the configured sigil forms.
//
// The longest matching prefix wins, with configuration order breaking ties. A numbered
// placeholder without digits has number zero.
//
// Takes text (string) which is the placeholder exactly as written.
//
// Returns PlaceholderReading which says what the placeholder holds.
// Returns bool which is false when no form reads the text.
func (placeholders Placeholders) Read(text string) (PlaceholderReading, bool) {
	var best PlaceholderReading
	found := false
	for _, sigil := range placeholders.Sigils {
		if found && len(sigil.Prefix) <= len(best.Sigil.Prefix) {
			continue
		}
		if reading, ok := sigil.read(text); ok {
			best, found = reading, true
		}
	}
	return best, found
}

// PlaceholderReading is what one placeholder, written in one of a dialect's forms, says
// about its parameter.
type PlaceholderReading struct {
	// Name is the parameter's name, for a named form.
	Name string

	// Type is the type a typed named form writes, such as UInt64 in {id:UInt64}, and is
	// empty otherwise.
	Type string

	// Sigil is the form the placeholder is written in.
	Sigil PlaceholderSigil

	// TypeOffset is the byte offset of Type within the placeholder, when Type is set.
	TypeOffset int

	// Number is the parameter's number, for a numbered form, or zero for a numbered form
	// written without its number, such as SQLite's bare ?, which takes the number after the
	// largest one before it.
	Number int
}

// isPlaceholderName reports whether text is a name a named placeholder may carry: a
// letter or underscore followed by letters, digits, underscores and dollar signs, as
// SQLite continues a name such as :price$usd.
//
// Only a token the dialect's lexer read as one placeholder is read, so a dialect whose
// names stop at '$' never offers one here.
//
// Takes text (string) which is the candidate name.
//
// Returns bool which is true for a name.
func isPlaceholderName(text string) bool {
	if text == "" {
		return false
	}
	for index, character := range text {
		if character == utf8.RuneError {
			return false
		}
		if character == '_' || unicode.IsLetter(character) || (index > 0 && (unicode.IsDigit(character) || character == '$')) {
			continue
		}
		return false
	}
	return true
}
