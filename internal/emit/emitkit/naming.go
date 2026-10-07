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

package emitkit

import (
	"maps"
	"slices"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// NameStyle holds the choices the naming plan makes as the style and the type mapping
// say.
type NameStyle struct {
	// NamedTypes reports whether the type mapping maps an enum or a struct type to a named
	// Go type the package declares, so the plan names it. A nil function names none, and
	// every enum and struct keeps the Go type the mapping gives it without a plan.
	NamedTypes func(sqlType sqlmodel.SQLType) bool

	// SQLConstantSuffix ends the name of every SQL constant, as emit.Style holds it.
	SQLConstantSuffix string

	// SQLConstantLowerCase lower-cases each part of a query's name between underscores in
	// the names of its SQL constant, count constant and allowed-columns variable, as
	// emit.Style holds it.
	SQLConstantLowerCase bool
}

// constantBase returns the name a query's package-level value names start with, followed
// by a suffix: the query's name in lower camel case keeping its words, such as getUser
// for GetUser, or with each part between underscores lower-cased, such as getuser, when
// the style says so.
//
// Takes name (string) which is the query's name.
// Takes suffix (string) which follows it.
//
// Returns string which is the name.
func (style NameStyle) constantBase(name, suffix string) string {
	if style.SQLConstantLowerCase {
		return goast.SnakeToCamelCaseSuffixed(name, suffix)
	}
	return goast.LowerCamelCaseSuffixed(name, suffix)
}

// nameScope is a set of names taken in one namespace, such as a generated package's
// package-level identifiers, from which each further name is claimed unique. A scope is
// not safe for concurrent use.
type nameScope struct {
	// parent holds immutable reservations shared by sibling scopes.
	parent *nameScope

	// taken holds the names already taken.
	taken map[string]struct{}

	// next maps a name that had to be numbered to the first number not yet tried for it.
	next map[string]int
}

// newNameScope returns a scope in which the given names are already taken.
//
// Takes names (...string) which are the names taken from the start.
//
// Returns *nameScope which is the scope.
func newNameScope(names ...string) *nameScope {
	taken := make(map[string]struct{}, len(names))
	for _, name := range names {
		taken[name] = struct{}{}
	}
	return &nameScope{taken: taken, next: make(map[string]int), parent: nil}
}

// claim takes a name in the scope: the name itself when it is free, and otherwise the
// first of name2, name3 and so on that is, in time that stays constant on average however
// many alike names the scope holds.
//
// Takes name (string) which is the name wanted.
//
// Returns string which is the name taken.
func (s *nameScope) claim(name string) string {
	return goast.ClaimUnique(name, s.next, s.take)
}

// has reports whether a name is already taken in the scope.
//
// Takes name (string) which is the name to check.
//
// Returns bool which is true when the name is taken.
func (s *nameScope) has(name string) bool {
	_, taken := s.taken[name]
	return taken || s.parent != nil && s.parent.has(name)
}

// take marks a name taken as it is, whether or not it already was.
//
// Takes name (string) which is the name to take.
//
// Returns bool which is true when the name was free.
func (s *nameScope) take(name string) bool {
	if s.has(name) {
		return false
	}
	s.taken[name] = struct{}{}
	return true
}

// names returns every name taken in the scope, sorted.
//
// Returns []string which holds the names.
func (s *nameScope) names() []string {
	return slices.Sorted(maps.Keys(s.taken))
}

// reservedNames holds the names one query's planning must avoid besides its own: those of
// the methods' bodies and those of the package.
type reservedNames struct {
	// PackageLevel is the package-wide scope the query's package-level names are claimed in.
	// A nil scope claims them in a scope of their own.
	PackageLevel *nameScope

	// Methods is the scope of the Queries method set the query's method is claimed in. A nil
	// scope leaves the method its name.
	Methods *nameScope

	// Receiver is the receiver of the Queries methods.
	Receiver string

	// Locals holds the names a method body declares or refers to besides the query's own,
	// which the query's arguments and locals avoid: the emitter's locals, and the
	// package-level names any body may refer to, such as helpers, models and named types.
	Locals *nameScope

	// Imports lists the qualifiers the generated files refer to imported packages by.
	Imports []string
}

// StyleNames returns the naming choices a style makes, with no named types: the SQL
// constants' suffix and case rule.
//
// Takes style (*emit.Style) which is the style. StyleNames does not change it.
//
// Returns NameStyle which holds the style's naming choices.
func StyleNames(style *emit.Style) NameStyle {
	return NameStyle{NamedTypes: nil, SQLConstantSuffix: style.SQLConstantSuffix, SQLConstantLowerCase: style.SQLConstantLowerCase}
}
