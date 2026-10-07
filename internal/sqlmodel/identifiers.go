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

package sqlmodel

import "strings"

const (
	// IdentifierFoldingNone keeps an unquoted identifier exactly as written.
	IdentifierFoldingNone IdentifierFolding = iota

	// IdentifierFoldingLower folds an unquoted identifier to lower case.
	IdentifierFoldingLower

	// IdentifierFoldingUpper folds an unquoted identifier to upper case.
	IdentifierFoldingUpper
)

var (
	// identifierFoldingNames names each IdentifierFolding, indexed by its value.
	identifierFoldingNames = [...]string{
		IdentifierFoldingNone:  "none",
		IdentifierFoldingLower: "lower",
		IdentifierFoldingUpper: "upper",
	}
)

// IdentifierFolding is how a dialect changes the letter case of an identifier written
// without quotes. A quoted identifier is never folded.
type IdentifierFolding uint8

// String returns the folding's name, such as lower.
//
// Returns string which is the name, or IdentifierFolding(n) for an undeclared value.
func (folding IdentifierFolding) String() string {
	return enumName(folding, identifierFoldingNames[:], "IdentifierFolding")
}

// IdentifierCase describes how a dialect treats the letter case of identifiers: how it
// folds an unquoted one, and whether names then match regardless of case.
//
// A dialect's parser applies the folding to the identifiers it reads, using whether each
// was quoted, so the names that reach the analyser are already canonical. The analyser
// keys the catalogue and compares names through Key, so it compares exactly in a
// case-sensitive dialect and ignores case in a case-insensitive one. Names that arrive
// from outside SQL, such as a configured search path or a column override, are folded
// with Fold first, as if written unquoted.
type IdentifierCase struct {
	// Unquoted is how the dialect folds an identifier written without quotes.
	Unquoted IdentifierFolding

	// Insensitive reports that the dialect matches identifiers regardless of letter case,
	// quoted or not.
	Insensitive bool
}

// Fold returns the canonical spelling of an identifier: the name folded as the dialect
// folds an unquoted identifier, or the name unchanged when it was quoted.
//
// Takes name (string) which is the identifier as written, without its quotes.
// Takes quoted (bool) which is true when the identifier was written quoted.
//
// Returns string which is the canonical spelling.
func (c IdentifierCase) Fold(name string, quoted bool) string {
	if quoted {
		return name
	}
	switch c.Unquoted {
	case IdentifierFoldingLower:
		return strings.ToLower(name)
	case IdentifierFoldingUpper:
		return strings.ToUpper(name)
	default:
		return name
	}
}

// Key returns the form of a canonical name the analyser compares and keys maps by: the
// name itself in a case-sensitive dialect, and its lower-case form in a case-insensitive
// one.
//
// Takes name (string) which is a canonical name, as Fold or the dialect's parser produced
// it.
//
// Returns string which is the comparison key.
func (c IdentifierCase) Key(name string) string {
	if c.Insensitive {
		return strings.ToLower(name)
	}
	return name
}

// Equal reports whether two canonical names name the same object.
//
// Takes left (string) which is the first canonical name.
// Takes right (string) which is the second canonical name.
//
// Returns bool which is true when the names' keys are equal.
func (c IdentifierCase) Equal(left, right string) bool {
	return c.Key(left) == c.Key(right)
}
