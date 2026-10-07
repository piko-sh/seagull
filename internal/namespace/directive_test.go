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

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetMatchClassifiesEveryKind(t *testing.T) {
	t.Parallel()

	set := Set{Canonical: "acme", Aliases: []string{"acmedb"}, Foreign: []string{"other"}}

	testCases := []struct {
		token string
		want  Kind
	}{
		{token: "acme", want: KindCanonical},
		{token: "acmedb", want: KindAlias},
		{token: "other", want: KindForeign},
		{token: "unrelated", want: KindNone},
		{token: "", want: KindNone},
		{token: "ACME", want: KindNone},
	}
	for _, testCase := range testCases {
		assert.Equalf(t, testCase.want, set.Match(testCase.token), "token %q", testCase.token)
	}
}

func TestSetAcceptsCanonicalAndAliasesOnly(t *testing.T) {
	t.Parallel()

	set := Set{Canonical: "acme", Aliases: []string{"acmedb"}, Foreign: []string{"other"}}

	assert.True(t, set.Accepts("acme"))
	assert.True(t, set.Accepts("acmedb"))
	assert.False(t, set.Accepts("other"))
	assert.False(t, set.Accepts("none"))
}

func TestSetQualifyRendersCanonical(t *testing.T) {
	t.Parallel()

	set := Set{Canonical: "acme", Aliases: []string{"acmedb"}, Foreign: nil}

	assert.Equal(t, "acme.query", set.Qualify("query"))
	assert.Equal(t, "acme.", set.Qualify(""))
}

func TestSetAcceptedListsCanonicalFirst(t *testing.T) {
	t.Parallel()

	set := Set{Canonical: "acme", Aliases: []string{"acmedb", "acme2"}, Foreign: []string{"other"}}

	assert.Equal(t, []string{"acme", "acmedb", "acme2"}, set.Accepted())
}

func TestValidPrefix(t *testing.T) {
	t.Parallel()

	valid := []string{"sgl", "a", "acme_db", "x1", "abcdefghijklmnopqrstuvwxyz012345"}
	invalid := []string{"", "1sgl", "_sgl", "Sgl", "sg-l", "sg.l", "abcdefghijklmnopqrstuvwxyz0123456"}

	for _, prefix := range valid {
		assert.Truef(t, ValidPrefix(prefix), "prefix %q", prefix)
	}
	for _, prefix := range invalid {
		assert.Falsef(t, ValidPrefix(prefix), "prefix %q", prefix)
	}
}
