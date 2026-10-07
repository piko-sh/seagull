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

package namespace

import "slices"

const (
	// maxPrefixLength is the longest directive prefix a Set accepts.
	maxPrefixLength = 32
)

// Kind classifies a token against a Set of directive prefixes.
type Kind uint8

const (
	// KindNone marks a token that is not a directive prefix of any kind.
	KindNone Kind = iota

	// KindCanonical marks the canonical prefix, the one diagnostics render.
	KindCanonical

	// KindAlias marks a prefix accepted as a synonym for the canonical prefix.
	KindAlias

	// KindForeign marks a prefix that is recognised only so it can be reported: a directive
	// written with it is not parsed.
	KindForeign
)

// Set holds the directive prefixes one generation run recognises: the canonical prefix
// that diagnostics render, the aliases accepted as synonyms, and the foreign prefixes
// reported but never parsed.
//
// A Set is plain data; callers build one per run and pass it to every site that
// recognises or renders a directive name.
type Set struct {
	// Canonical is the prefix every directive name is rendered with, such as the sgl in
	// sgl.query.
	Canonical string

	// Aliases lists prefixes accepted as synonyms for Canonical.
	Aliases []string

	// Foreign lists prefixes that belong to another tool; a directive written with one is
	// reported and otherwise treated as prose.
	Foreign []string
}

// Match classifies token against the set.
//
// Takes token (string) which is the identifier before the dot or parenthesis of a
// candidate directive.
//
// Returns Kind which is KindCanonical, KindAlias, KindForeign or KindNone.
func (s Set) Match(token string) Kind {
	switch {
	case token == "":
		return KindNone
	case token == s.Canonical:
		return KindCanonical
	case slices.Contains(s.Aliases, token):
		return KindAlias
	case slices.Contains(s.Foreign, token):
		return KindForeign
	default:
		return KindNone
	}
}

// Accepts reports whether token is the canonical prefix or one of its aliases, the
// prefixes whose directives are parsed.
//
// Takes token (string) which is the candidate prefix.
//
// Returns bool which is true for the canonical prefix and every alias.
func (s Set) Accepts(token string) bool {
	kind := s.Match(token)
	return kind == KindCanonical || kind == KindAlias
}

// Qualify returns the full directive name for a role, rendered with the canonical prefix,
// so "query" becomes "sgl.query" under the default set.
//
// Takes role (string) which is the directive role without its prefix.
//
// Returns string which is the canonical, namespace-qualified directive name.
func (s Set) Qualify(role string) string {
	return s.Canonical + "." + role
}

// Accepted returns the canonical prefix followed by its aliases.
//
// Returns []string which holds every prefix whose directives are parsed, canonical first.
func (s Set) Accepted() []string {
	accepted := make([]string, 0, 1+len(s.Aliases))
	accepted = append(accepted, s.Canonical)
	return append(accepted, s.Aliases...)
}

// ValidPrefix reports whether prefix has the shape every directive prefix must have: a
// lower-case ASCII letter followed by up to 31 lower-case letters, digits or underscores.
//
// Takes prefix (string) which is the candidate prefix.
//
// Returns bool which is true when prefix is well formed.
func ValidPrefix(prefix string) bool {
	if prefix == "" || len(prefix) > maxPrefixLength {
		return false
	}
	if prefix[0] < 'a' || prefix[0] > 'z' {
		return false
	}
	for index := 1; index < len(prefix); index++ {
		character := prefix[index]
		isLower := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		if !isLower && !isDigit && character != '_' {
			return false
		}
	}
	return true
}
