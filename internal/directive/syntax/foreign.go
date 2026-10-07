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

package syntax

import (
	"strings"

	"seagull.sh/seagull/internal/namespace"
)

// ForeignDirective records a directive written with a foreign prefix: one that belongs to
// another tool and is reported rather than parsed.
type ForeignDirective struct {
	// Prefix is the foreign prefix the directive was written with.
	Prefix string

	// Name is the directive name as written, such as other.query, or the bare prefix for a
	// call written as other(...).
	Name string

	// Line is the 1-based line number of the directive.
	Line int
}

// ForeignDirectiveCall reports whether a trimmed comment body opens a directive call
// written with a foreign prefix, as in `other.query(` or `other(`.
//
// Takes body (string) which is the comment text after the comment prefix.
// Takes prefixes (namespace.Set) which names the foreign prefixes to recognise.
//
// Returns ForeignDirective which describes the call, with Line left zero.
// Returns bool which is true when body opens a foreign directive call.
func ForeignDirectiveCall(body string, prefixes namespace.Set) (ForeignDirective, bool) {
	prefix := leadingIdentifier(body)
	if prefixes.Match(prefix) != namespace.KindForeign {
		return ForeignDirective{}, false
	}
	rest := body[len(prefix):]
	if strings.HasPrefix(strings.TrimLeft(rest, " \t"), "(") {
		return ForeignDirective{Prefix: prefix, Name: prefix, Line: 0}, true
	}
	afterDot, hasDot := strings.CutPrefix(rest, ".")
	if !hasDot {
		return ForeignDirective{}, false
	}
	role := leadingIdentifier(afterDot)
	if role == "" || !strings.HasPrefix(strings.TrimLeft(afterDot[len(role):], " \t"), "(") {
		return ForeignDirective{}, false
	}
	return ForeignDirective{Prefix: prefix, Name: prefix + "." + role, Line: 0}, true
}
