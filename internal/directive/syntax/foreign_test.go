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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/namespace"
)

func TestParseMigrationReportsEveryShapeOfForeignDirective(t *testing.T) {
	t.Parallel()

	prefixes := namespace.Set{Canonical: "acme", Aliases: nil, Foreign: []string{"other"}}
	content := "-- other.query(GetUser, one)\n" +
		"-- acme.query(GetUser, one)\n" +
		"SELECT 1;\n" +
		"  --   other (name: Foo)\n" +
		"-- other keeps this fast\n" +
		"-- other.migration (readonly: true)\n" +
		"-- other.\n"

	directives, _ := ParseMigration(content, NeutralLexicon(), prefixes, "")
	found := directives.Foreign

	require.Len(t, found, 3)
	assert.Equal(t, ForeignDirective{Prefix: "other", Name: "other.query", Line: 1}, found[0])
	assert.Equal(t, ForeignDirective{Prefix: "other", Name: "other", Line: 4}, found[1])
	assert.Equal(t, ForeignDirective{Prefix: "other", Name: "other.migration", Line: 6}, found[2])
}

func TestParseMigrationReportsNoForeignDirectiveWithoutForeignPrefixes(t *testing.T) {
	t.Parallel()

	prefixes := namespace.Set{Canonical: "acme", Aliases: nil, Foreign: nil}

	directives, _ := ParseMigration("-- other.query(GetUser, one)", NeutralLexicon(), prefixes, "")
	assert.Nil(t, directives.Foreign)
}

func TestForeignDirectiveCallRejectsNonCalls(t *testing.T) {
	t.Parallel()

	prefixes := namespace.Set{Canonical: "acme", Aliases: nil, Foreign: []string{"other"}}

	for _, body := range []string{"", "acme.query(x)", "other", "other.query", "other.1(x)", "otherwise(x)", "$1 as other.param(x)"} {
		_, ok := ForeignDirectiveCall(body, prefixes)
		assert.Falsef(t, ok, "body %q", body)
	}
}
