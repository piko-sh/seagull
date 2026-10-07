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
	"strings"
	"unicode"

	"piko.sh/goastutil"
)

const (
	// unnamedExportedIdentifier is the exported identifier used for a name made only of
	// characters no Go identifier may contain.
	unnamedExportedIdentifier = "Unnamed"

	// unnamedIdentifier is the unexported identifier used for a name made only of characters
	// no Go identifier may contain.
	unnamedIdentifier = "unnamed"
)

var (
	// commonInitialisms maps lowercase initialisations to their canonical upper-case forms
	// per Go naming conventions.
	commonInitialisms = map[string]string{
		"id":    "ID",
		"ids":   "IDs",
		"url":   "URL",
		"uri":   "URI",
		"api":   "API",
		"sql":   "SQL",
		"http":  "HTTP",
		"https": "HTTPS",
		"ip":    "IP",
		"css":   "CSS",
		"html":  "HTML",
		"json":  "JSON",
		"xml":   "XML",
		"ssh":   "SSH",
		"tls":   "TLS",
		"tcp":   "TCP",
		"udp":   "UDP",
		"cpu":   "CPU",
		"gpu":   "GPU",
		"ram":   "RAM",
		"uuid":  "UUID",
		"uid":   "UID",
		"ascii": "ASCII",
		"utf8":  "UTF8",
		"eof":   "EOF",
		"ttl":   "TTL",
		"acl":   "ACL",
		"pk":    "PK",
		"fk":    "FK",
	}
)

// SnakeToPascalCase converts a snake_case SQL identifier to PascalCase Go identifier,
// applying Go initialism conventions.
//
// A SQL name need not be a legal Go identifier stem: a quoted identifier or an alias may
// hold any character at all, so "2fa_enabled" would give "2faEnabled", which starts with
// a digit, and "my-query" would give "My-query", which is two tokens. The name is
// therefore split on every rune that cannot appear in an identifier, not on underscores
// alone, and the joined result is made into an exported identifier.
//
// Takes name (string) which is the snake_case identifier to convert.
//
// Returns string which is the PascalCase Go identifier.
func SnakeToPascalCase(name string) string {
	joined := joinPascalSegments(name)
	if joined == "" {
		if name == "" {
			return ""
		}

		return withUnnamedFallback(goastutil.SanitiseGoExportedIdentifier(name), unnamedExportedIdentifier)
	}

	return goastutil.SanitiseGoExportedIdentifier(joined)
}

// PascalCaseSuffix converts a SQL name for use after an existing identifier prefix.
//
// It preserves leading digits because the result need not stand alone. Names with no
// letters or digits produce an empty string.
//
// Takes name (string) which is the SQL name to convert.
//
// Returns string which is the PascalCase suffix, or empty when the name has no letter or
// digit.
func PascalCaseSuffix(name string) string {
	return joinPascalSegments(name)
}

// SnakeToCamelCase converts a snake_case SQL identifier to camelCase Go identifier,
// applying Go initialism conventions for non-leading segments.
//
// As with SnakeToPascalCase, the name is split on every rune that cannot appear in an
// identifier, a name that leaves nothing to join falls back to the shared kit, and the
// result is passed through sanitiseGoIdentifier. The reserved word guard matters more
// here than in the PascalCase form: a camelCase name keeps its lower-case leading
// segment, so a column called "range" or "string" would otherwise emit a declaration that
// either does not compile or shadows a predeclared identifier for the rest of the scope.
//
// Takes name (string) which is the snake_case identifier to convert.
//
// Returns string which is the camelCase Go identifier.
func SnakeToCamelCase(name string) string {
	return SnakeToCamelCaseSuffixed(name, "")
}

// SnakeToCamelCaseSuffixed converts a SQL name to a camelCase Go identifier, adding the
// suffix before checking for keyword conflicts.
//
// Takes name (string) which is the snake_case identifier to convert.
// Takes suffix (string) which ends the identifier.
//
// Returns string which is the camelCase Go identifier.
func SnakeToCamelCaseSuffixed(name, suffix string) string {
	segments := splitIdentifierSegments(name)
	var builder strings.Builder
	builder.Grow(len(name) + len(suffix))

	for index, segment := range segments {
		lower := strings.ToLower(segment)

		if index == 0 {
			builder.WriteString(lower)
			continue
		}

		if canonical, exists := commonInitialisms[lower]; exists {
			builder.WriteString(canonical)
			continue
		}

		runes := []rune(lower)
		runes[0] = unicode.ToUpper(runes[0])
		builder.WriteString(string(runes))
	}

	if builder.Len() == 0 && name != "" {
		return withUnnamedFallback(goastutil.SanitiseGoIdentifier(name), unnamedIdentifier) + suffix
	}
	builder.WriteString(suffix)
	return sanitiseGoIdentifier(builder.String())
}

// LowerCamelCaseSuffixed converts a name to lower camel case and appends a suffix.
//
// It preserves mixed-case word boundaries, so HTTPServer becomes httpServer.
//
// Takes name (string) which is the name to convert.
// Takes suffix (string) which follows the converted name.
//
// Returns string which is the camelCase Go identifier, made legal as SnakeToCamelCase
// makes its result.
func LowerCamelCaseSuffixed(name, suffix string) string {
	pascal := joinPascalSegments(name)
	if pascal == "" {
		return SnakeToCamelCaseSuffixed(name, suffix)
	}
	runes := []rune(pascal)
	leading := 0
	for leading < len(runes) && unicode.IsUpper(runes[leading]) {
		leading++
	}
	if leading > 1 && leading < len(runes) && unicode.IsLower(runes[leading]) {
		leading--
	}
	for index := range max(leading, 1) {
		runes[index] = unicode.ToLower(runes[index])
	}
	return sanitiseGoIdentifier(string(runes) + suffix)
}

// DisambiguateGoFieldNames converts an ordered list of snake_case SQL names into the
// matching ordered list of PascalCase Go field identifiers, suffixing collisions.
//
// Takes names ([]string) which are the source SQL names in declaration order.
//
// Returns []string which are the disambiguated PascalCase identifiers in the same order.
func DisambiguateGoFieldNames(names []string) []string {
	return disambiguateGoFieldNames(names, SnakeToPascalCase)
}

// DisambiguateGoFieldNamesCamelCase converts an ordered list of snake_case SQL names into
// the matching ordered list of camelCase Go identifiers, suffixing collisions.
//
// Takes names ([]string) which are the source SQL names in declaration order.
//
// Returns []string which are the disambiguated camelCase identifiers in the same order.
func DisambiguateGoFieldNamesCamelCase(names []string) []string {
	return disambiguateGoFieldNames(names, SnakeToCamelCase)
}

// joinPascalSegments joins the PascalCase form of each identifier segment of a name: a
// common initialism in its canonical form, a mixed-case segment with its first letter
// upper-cased and its interior kept, and any other segment lower-cased with its first
// letter upper-cased.
//
// Takes name (string) which is the SQL name to convert.
//
// Returns string which is the joined segments, empty when the name has none.
func joinPascalSegments(name string) string {
	segments := splitIdentifierSegments(name)
	var builder strings.Builder
	builder.Grow(len(name))

	for _, segment := range segments {
		lower := strings.ToLower(segment)
		if canonical, exists := commonInitialisms[lower]; exists {
			builder.WriteString(canonical)
			continue
		}

		if isMixedCaseSegment(segment) {
			runes := []rune(segment)
			runes[0] = unicode.ToUpper(runes[0])
			builder.WriteString(string(runes))
			continue
		}

		runes := []rune(lower)
		runes[0] = unicode.ToUpper(runes[0])
		builder.WriteString(string(runes))
	}

	return builder.String()
}

// withUnnamedFallback replaces the shared kit's last-resort identifier with this
// generator's own, so a name made only of punctuation never takes the kit's default
// spelling into generated code.
//
// Takes sanitised (string) which is the identifier the shared kit produced.
// Takes fallback (string) which is the identifier to use instead of the kit's default.
//
// Returns string which is sanitised, or fallback when sanitised is the kit's default.
func withUnnamedFallback(sanitised, fallback string) string {
	if strings.EqualFold(sanitised, goastutil.DefaultGoIdentifier) {
		return fallback
	}
	return sanitised
}

// isMixedCaseSegment reports whether a segment already carries both an upper-case and a
// lower-case letter, marking it as a camelCase token (such as "jobCount") whose interior
// capitalisation must be preserved rather than folded away.
//
// Takes segment (string) which is a single separator-delimited identifier segment.
//
// Returns bool which is true when the segment mixes upper- and lower-case letters.
func isMixedCaseSegment(segment string) bool {
	var hasLower, hasUpper bool
	for _, character := range segment {
		switch {
		case unicode.IsLower(character):
			hasLower = true
		case unicode.IsUpper(character):
			hasUpper = true
		}
	}
	return hasLower && hasUpper
}

// splitIdentifierSegments splits a SQL name into the segments a Go identifier is built
// from.
//
// Takes name (string) which is the raw SQL identifier.
//
// Returns []string which are the non-empty identifier segments in source order.
func splitIdentifierSegments(name string) []string {
	return strings.FieldsFunc(name, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
}

// sanitiseGoIdentifier ensures a converted name is a legal Go identifier stem.
//
// goastutil repairs anything the conversion could not fix, prefixes a stem that starts
// with a digit, and suffixes a name that landed on a Go keyword or a predeclared
// identifier, since a column called "range" emits source that does not compile and one
// called "string" shadows the type for the rest of the scope. An empty name is left empty
// so callers that intentionally pass an empty string, for example an anonymous field or
// result, are not given a spurious underscore.
//
// Takes name (string) which is the candidate Go identifier.
//
// Returns string which is a legal Go identifier stem.
func sanitiseGoIdentifier(name string) string {
	if name == "" {
		return ""
	}

	return goastutil.SanitiseGoIdentifier(name)
}

// disambiguateGoFieldNames converts an ordered list of snake_case SQL names into the
// matching ordered list of Go identifiers using the supplied case converter.
//
// A numeric suffix is appended to any name that would otherwise collide with an earlier
// one. Distinct SQL names can fold onto the same Go identifier, for example "foo_bar" and
// "foo__bar" both become "FooBar", which would be a duplicate-field compile error. The
// suffix keeps each field unique while preserving source order so the struct fields and
// the scan targets derived from the same ordered list stay in lockstep.
//
// The suffix search continues past a suffixed candidate that itself collides, so a
// literal column whose converted form equals an already-emitted suffix (for example
// "foo2" alongside a second "foo" that became "Foo2") still receives a distinct
// identifier.
//
// Takes names ([]string) which are the source SQL names in declaration order.
// Takes convert (func(string) string) which maps a snake_case name to its Go identifier.
//
// Returns []string which are the disambiguated Go identifiers in the same order.
func disambiguateGoFieldNames(names []string, convert func(string) string) []string {
	result := make([]string, len(names))
	unique := NewUniqueNames(len(names))
	for index, name := range names {
		result[index] = unique.Claim(convert(name))
	}
	return result
}
