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

package dialect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
)

func TestPlaceholderSigilRender(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		expected string
		sigil    dialect.PlaceholderSigil
	}{
		{
			name:     "a numbered sigil writes the number after the prefix",
			sigil:    dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
			expected: "$3",
		},
		{
			name:     "a multi-character prefix is written whole",
			sigil:    dialect.PlaceholderSigil{Prefix: "@p", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
			expected: "@p3",
		},
		{
			name:     "an anonymous sigil writes the prefix alone",
			sigil:    dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous},
			expected: "?",
		},
		{
			name:     "a named sigil writes the name",
			sigil:    dialect.PlaceholderSigil{Prefix: ":", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
			expected: ":total",
		},
		{
			name:     "a typed named sigil writes the name, the type and the suffix",
			sigil:    dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed},
			expected: "{total:UInt64}",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, testCase.sigil.Render(3, "total", "UInt64"))
		})
	}
}

func TestPlaceholdersBinding(t *testing.T) {
	t.Parallel()

	numbered := dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}
	anonymous := dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous}
	named := dialect.Placeholders{
		Sigils:     nil,
		Execution:  dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed},
		Positional: numbered,
		NamePrefix: "p_",
	}
	positional := dialect.Placeholders{
		Sigils:     nil,
		Execution:  anonymous,
		Positional: dialect.PlaceholderSigil{},
		NamePrefix: "",
	}

	require.True(t, named.BindsByName())
	require.Equal(t, "p_limit", named.BoundName("limit"))
	require.Equal(t, numbered, named.PositionalForm())
	require.False(t, positional.BindsByName())
	require.Equal(t, "limit", positional.BoundName("limit"))
	require.Equal(t, anonymous, positional.PositionalForm())
}

func TestPlaceholdersRead(t *testing.T) {
	t.Parallel()

	dollar := dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}
	colon := dialect.PlaceholderSigil{Prefix: ":", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed}
	question := dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous}
	at := dialect.PlaceholderSigil{Prefix: "@", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed}
	atP := dialect.PlaceholderSigil{Prefix: "@p", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}
	braced := dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed}
	placeholders := dialect.Placeholders{
		Sigils:     []dialect.PlaceholderSigil{dollar, colon, question, at, atP, braced},
		Execution:  dollar,
		Positional: dialect.PlaceholderSigil{},
		NamePrefix: "",
	}

	testCases := []struct {
		name     string
		text     string
		expected dialect.PlaceholderReading
		ok       bool
	}{
		{name: "a numbered form gives its number", text: "$12", ok: true,
			expected: dialect.PlaceholderReading{Name: "", Type: "", Sigil: dollar, TypeOffset: 0, Number: 12}},
		{name: "a named form gives its name", text: ":email", ok: true,
			expected: dialect.PlaceholderReading{Name: "email", Type: "", Sigil: colon, TypeOffset: 0, Number: 0}},
		{name: "an anonymous form gives nothing", text: "?", ok: true,
			expected: dialect.PlaceholderReading{Name: "", Type: "", Sigil: question, TypeOffset: 0, Number: 0}},
		{name: "the longest prefix wins", text: "@p3", ok: true,
			expected: dialect.PlaceholderReading{Name: "", Type: "", Sigil: atP, TypeOffset: 0, Number: 3}},
		{name: "a shorter prefix reads what the longer cannot", text: "@pending", ok: true,
			expected: dialect.PlaceholderReading{Name: "pending", Type: "", Sigil: at, TypeOffset: 0, Number: 0}},
		{name: "a typed form gives its name and type", text: "{ids:Array(UInt64)}", ok: true,
			expected: dialect.PlaceholderReading{Name: "ids", Type: "Array(UInt64)", Sigil: braced, TypeOffset: 5, Number: 0}},
		{name: "a typed form without its type is not read", text: "{ids}", ok: false,
			expected: dialect.PlaceholderReading{}},
		{name: "a numbered form without its number reads with number zero", text: "$", ok: true,
			expected: dialect.PlaceholderReading{Name: "", Type: "", Sigil: dollar, TypeOffset: 0, Number: 0}},
		{name: "a zero number is not read", text: "$0", ok: false,
			expected: dialect.PlaceholderReading{}},
		{name: "a name may not start with a digit", text: ":1st", ok: false,
			expected: dialect.PlaceholderReading{}},
		{name: "a name continues through a dollar sign", text: ":price$usd", ok: true,
			expected: dialect.PlaceholderReading{Name: "price$usd", Type: "", Sigil: colon, TypeOffset: 0, Number: 0}},
		{name: "a name may end with a dollar sign", text: "@total$", ok: true,
			expected: dialect.PlaceholderReading{Name: "total$", Type: "", Sigil: at, TypeOffset: 0, Number: 0}},
		{name: "a name may not start with a dollar sign", text: ":$usd", ok: false,
			expected: dialect.PlaceholderReading{}},
		{name: "text in no form is not read", text: "#x", ok: false,
			expected: dialect.PlaceholderReading{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reading, ok := placeholders.Read(testCase.text)
			require.Equal(t, testCase.ok, ok)
			if testCase.ok {
				require.Equal(t, testCase.expected, reading)
			}
		})
	}
}
