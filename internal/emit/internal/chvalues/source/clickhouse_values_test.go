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

package source

import (
	"database/sql/driver"
	"errors"
	"math"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type valuesBlob []byte

type valuesColour string

type valuesMood int8

type valuesHostileCount int

type valuesLabel struct {
	text string
}

type valuesPointerLabel struct {
	text string
}

type valuesFallible struct {
	err  error
	text string
}

type valuesIdentifier [4]byte

type valuesPoint struct {
	X int
	Y string
}

type valuesHidden struct {
	visible int
	Name    string
}

func (mood valuesMood) String() string {
	return [...]string{"sad", "happy"}[mood]
}

func (valuesHostileCount) String() string {
	return "1],evil['"
}

func (label valuesLabel) String() string {
	return label.text
}

func (label *valuesPointerLabel) String() string {
	return label.text
}

func (fallible valuesFallible) String() (string, error) {
	return fallible.text, fallible.err
}

func (identifier valuesIdentifier) String() string {
	return "id-" + string(rune('a'+identifier[0]))
}

func refusal(t *testing.T, text string) string {
	t.Helper()
	require.True(t, strings.HasSuffix(text, "\t"), "a refusal ends in a tab: %q", text)
	require.NotContains(t, strings.TrimSuffix(text, "\t"), "\t")
	require.NotContains(t, text, "\n")
	return strings.TrimSuffix(text, "\t")
}

func TestClickHouseValueWritesAWholeParameter(t *testing.T) {
	t.Parallel()

	backslash := `a\b`
	moment := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	var nilString *string
	var nilInterface any
	plusTwo := time.FixedZone("plus two", 2*3600)

	testCases := []struct {
		value any
		name  string
		shape string
		want  string
	}{
		{name: "nil is the NULL marker", value: nil, shape: "q", want: `\N`},
		{name: "a nil pointer is the NULL marker", value: nilString, shape: "q", want: `\N`},
		{name: "a pointer to a nil interface is the NULL marker", value: &nilInterface, shape: "n", want: `\N`},
		{name: "a string is unchanged", value: "plain", shape: "q", want: "plain"},
		{name: "a backslash is escaped", value: backslash, shape: "q", want: `a\\b`},
		{name: "a pointer is followed", value: &backslash, shape: "q", want: `a\\b`},
		{name: "a tab, a newline, a carriage return and a NUL are escaped", value: "a\tb\nc\rd\x00e", shape: "q", want: `a\tb\nc\rd\0e`},
		{name: "text that looks like the NULL marker is escaped", value: `\N`, shape: "q", want: `\\N`},
		{name: "a quote needs no escaping in a whole value", value: "it's", shape: "q", want: "it's"},
		{name: "an integer", value: 42, shape: "n", want: "42"},
		{name: "an unsigned integer", value: uint64(18446744073709551615), shape: "n", want: "18446744073709551615"},
		{name: "a float", value: -1.5, shape: "n", want: "-1.5"},
		{name: "a 32-bit float keeps its own digits", value: float32(0.1), shape: "n", want: "0.1"},
		{name: "not a number", value: math.NaN(), shape: "n", want: "nan"},
		{name: "infinity", value: math.Inf(1), shape: "n", want: "inf"},
		{name: "negative infinity", value: math.Inf(-1), shape: "n", want: "-inf"},
		{name: "a boolean", value: true, shape: "n", want: "true"},
		{name: "a wide integer", value: new(big.Int).Lsh(big.NewInt(1), 100), shape: "n", want: "1267650600228229401496703205376"},
		{name: "a number written as text", value: "1.50", shape: "n", want: "1.50"},
		{name: "bytes are text", value: []byte(`x\y`), shape: "q", want: `x\\y`},
		{name: "named bytes are text", value: valuesBlob("blob"), shape: "q", want: "blob"},
		{name: "a named string is text", value: valuesColour("red"), shape: "q", want: "red"},
		{name: "a stringer is its text", value: valuesLabel{text: `on\off`}, shape: "q", want: `on\\off`},
		{name: "a pointer-receiver stringer is its text", value: &valuesPointerLabel{text: "mine"}, shape: "q", want: "mine"},
		{name: "a named number with a String method is its text as text", value: valuesMood(1), shape: "q", want: "happy"},
		{name: "a named number with a String method is its number as a number", value: valuesMood(1), shape: "n", want: "1"},
		{name: "an IP address", value: netip.MustParseAddr("192.0.2.1"), shape: "q", want: "192.0.2.1"},
		{name: "a fallible stringer is its text", value: valuesFallible{text: "ok"}, shape: "q", want: "ok"},
		{name: "a date is the calendar date in the time's own location", value: time.Date(2001, 2, 3, 0, 30, 0, 0, plusTwo), shape: "d", want: "2001-02-03"},
		{name: "a date and time is Unix seconds", value: moment, shape: "t", want: "981173106"},
		{name: "a date and time drops a fraction it has no room for", value: moment.Add(500 * time.Millisecond), shape: "t", want: "981173106"},
		{name: "a date and time with three digits", value: moment.Add(123456789 * time.Nanosecond), shape: "t3", want: "981173106.123"},
		{name: "a date and time with nine digits", value: moment.Add(123456789 * time.Nanosecond), shape: "t9", want: "981173106.123456789"},
		{name: "a date and time keeps its moment in any location", value: moment.In(plusTwo), shape: "t", want: "981173106"},
		{name: "a date and time before 1970", value: time.Date(1969, 12, 31, 23, 59, 59, 500000000, time.UTC), shape: "t3", want: "-0.500"},
		{name: "a pointer to a time is the time", value: &moment, shape: "t", want: "981173106"},
		{name: "the zero time is a time like any other", value: time.Time{}, shape: "d", want: "0001-01-01"},
		{name: "the zero time is a moment like any other", value: time.Time{}, shape: "t", want: "-62135596800"},
		{name: "an unknown shape writes a number bare", value: 7, shape: "a", want: "7"},
		{name: "an unknown shape writes a time by its own precision", value: moment.Add(time.Millisecond), shape: "a", want: "981173106.001"},
		{name: "an unknown shape writes text escaped", value: "a\tb", shape: "a", want: `a\tb`},
		{name: "an array", value: []uint64{1, 2}, shape: "[n]", want: "[1,2]"},
		{name: "a nil slice is an empty array", value: []string(nil), shape: "[q]", want: "[]"},
		{name: "an array of text is quoted", value: []string{`a\b`, "c'd", "e\tf"}, shape: "[q]", want: `['a\\b','c\'d','e\tf']`},
		{name: "an array of nullable numbers", value: []*int{new(5), nil}, shape: "[n]", want: "[5,NULL]"},
		{name: "an array of UUIDs is quoted", value: []valuesIdentifier{{0}, {1}}, shape: "[q]", want: "['id-a','id-b']"},
		{name: "an array of IP addresses is quoted", value: []net.IP{net.IPv4(10, 0, 0, 1)}, shape: "[q]", want: "['10.0.0.1']"},
		{name: "an array of byte arrays is nested arrays", value: [][]byte{{1, 2}, {3}}, shape: "[[n]]", want: "[[1,2],[3]]"},
		{name: "an array of wide integers is bare", value: []*big.Int{big.NewInt(-3)}, shape: "[n]", want: "[-3]"},
		{name: "an array of dates", value: []time.Time{time.Date(2001, 2, 3, 23, 0, 0, 0, plusTwo)}, shape: "[d]", want: "['2001-02-03']"},
		{name: "an array of dates and times", value: []time.Time{moment}, shape: "[t]", want: "['981173106']"},
		{name: "a map in a fixed order", value: map[string]int{"b": 2, "a": 1, "c": 3}, shape: "{q:n}", want: "{'a':1,'b':2,'c':3}"},
		{name: "a nil map is an empty map", value: map[string]int(nil), shape: "{q:n}", want: "{}"},
		{name: "a map of arrays", value: map[string][]string{"k": {"v"}}, shape: "{q:[q]}", want: "{'k':['v']}"},
		{name: "a tuple from a struct", value: valuesPoint{X: 1, Y: "a"}, shape: "(n,q)", want: "(1, 'a')"},
		{name: "a tuple from a map by field names", value: map[string]any{"y": "b", "x": 2}, shape: "('x'=n,'y'=q)", want: "(2, 'b')"},
		{name: "a tuple from a map leaves a missing field NULL", value: map[string]any{"x": 2}, shape: "('x'=n,'y'=q)", want: "(2, NULL)"},
		{name: "a tuple field name may hold a quote", value: map[string]int{"it's": 4}, shape: "('it''s'=n)", want: "(4)"},
		{name: "a tuple from a slice", value: []any{1, "a"}, shape: "(n,q)", want: "(1, 'a')"},
		{name: "an array of tuples", value: []valuesPoint{{X: 1, Y: "a"}}, shape: "[(n,q)]", want: "[(1, 'a')]"},
		{name: "an unknown shape writes a slice as an array", value: []any{1, "a", nil, []int{2}}, shape: "a", want: "[1,'a',NULL,[2]]"},
		{name: "an unknown shape writes a struct as a tuple", value: valuesPoint{X: 3, Y: "z"}, shape: "a", want: "(3, 'z')"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglClickHouseValue(testCase.value, testCase.shape))
		})
	}
}

func TestClickHouseValueWritesADurationAsTheTimeClickHouseParses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value any
		name  string
		shape string
		want  string
	}{
		{name: "whole seconds", value: 12*time.Hour + 34*time.Minute + 56*time.Second, shape: "q", want: "12:34:56"},
		{name: "zero", value: time.Duration(0), shape: "q", want: "0:00:00"},
		{name: "more than a day", value: 999*time.Hour + 59*time.Minute + 59*time.Second, shape: "q", want: "999:59:59"},
		{name: "a fraction of a second", value: time.Hour + 500*time.Millisecond, shape: "q", want: "1:00:00.5"},
		{name: "a negative span with microseconds", value: -(100*time.Hour + time.Microsecond), shape: "q", want: "-100:00:00.000001"},
		{name: "nanoseconds", value: time.Nanosecond, shape: "q", want: "0:00:00.000000001"},
		{name: "a pointer is followed", value: new(2 * time.Minute), shape: "q", want: "0:02:00"},
		{name: "an array of durations", value: []time.Duration{time.Second, -time.Second}, shape: "[q]", want: "['0:00:01','-0:00:01']"},
		{name: "a duration bound as a number is its nanoseconds", value: time.Second, shape: "n", want: "1000000000"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglClickHouseValue(testCase.value, testCase.shape))
		})
	}
}

func TestClickHouseValueWritesAnAddressAsTheTypeHoldsIt(t *testing.T) {
	t.Parallel()

	plain := netip.MustParseAddr("10.0.0.1")
	mapped := netip.MustParseAddr("::ffff:10.0.0.9")
	wide := netip.MustParseAddr("2001:db8::1")
	var missing *netip.Addr

	testCases := []struct {
		value any
		name  string
		shape string
		want  string
	}{
		{name: "an IPv4 address for IPv4", value: plain, shape: "4", want: "10.0.0.1"},
		{name: "an IPv4-mapped address for IPv4 is unmapped", value: mapped, shape: "4", want: "10.0.0.9"},
		{name: "an IPv4 address for either version", value: plain, shape: "6", want: "10.0.0.1"},
		{name: "an IPv4-mapped address for either version keeps its form", value: mapped, shape: "6", want: "::ffff:10.0.0.9"},
		{name: "an IPv6 address for either version", value: wide, shape: "6", want: "2001:db8::1"},
		{name: "an address loses its zone", value: netip.MustParseAddr("fe80::1%eth0"), shape: "6", want: "fe80::1"},
		{name: "a pointer to an address", value: &mapped, shape: "4", want: "10.0.0.9"},
		{name: "a nil pointer is the NULL marker", value: missing, shape: "4", want: `\N`},
		{name: "a four-byte net.IP", value: net.IP{10, 0, 0, 2}, shape: "4", want: "10.0.0.2"},
		{name: "a sixteen-byte net.IP holding IPv4", value: net.ParseIP("10.0.0.3"), shape: "4", want: "10.0.0.3"},
		{name: "a sixteen-byte net.IP holding IPv4 for either version", value: net.ParseIP("10.0.0.3"), shape: "6", want: "::ffff:10.0.0.3"},
		{name: "text is left for the server to parse", value: "::ffff:10.0.0.4", shape: "4", want: "::ffff:10.0.0.4"},
		{name: "an array of addresses", value: []netip.Addr{plain, mapped}, shape: "[4]", want: "['10.0.0.1','10.0.0.9']"},
		{name: "an array of nullable addresses", value: []*netip.Addr{&mapped, nil}, shape: "[4]", want: "['10.0.0.9',NULL]"},
		{name: "a named tuple of both versions", value: map[string]any{"near": mapped, "far": wide}, shape: "('near'=4,'far'=6)",
			want: "('10.0.0.9', '2001:db8::1')"},
		{name: "a map of addresses", value: map[string]netip.Addr{"k": mapped}, shape: "{q:4}", want: "{'k':'10.0.0.9'}"},
		{name: "an unknown shape writes an address as its text", value: mapped, shape: "a", want: "::ffff:10.0.0.9"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglClickHouseValue(testCase.value, testCase.shape))
		})
	}
}

func TestClickHouseValueRefusesAnAddressTheTypeCannotHold(t *testing.T) {
	t.Parallel()

	zero := netip.Addr{}
	var empty net.IP

	testCases := []struct {
		value  any
		name   string
		shape  string
		reason string
	}{
		{name: "the zero address for IPv4", value: zero, shape: "4", reason: "the zero netip.Addr holds no address"},
		{name: "the zero address for either version", value: zero, shape: "6", reason: "the zero netip.Addr holds no address"},
		{name: "a pointer to the zero address for a nullable", value: &zero, shape: "4", reason: "the zero netip.Addr holds no address"},
		{name: "an IPv6 address for IPv4", value: netip.MustParseAddr("2001:db8::1"), shape: "4",
			reason: "2001:db8::1 is an IPv6 address, which an IPv4 value cannot hold"},
		{name: "a nil net.IP", value: empty, shape: "6", reason: "an empty net.IP holds no address"},
		{name: "a pointer to a nil net.IP", value: &empty, shape: "4", reason: "an empty net.IP holds no address"},
		{name: "a net.IP of the wrong length", value: net.IP{10, 0, 1}, shape: "6", reason: "a net.IP of 3 bytes is not an IP address"},
		{name: "an IPv6 net.IP for IPv4", value: net.ParseIP("2001:db8::2"), shape: "4", reason: "which an IPv4 value cannot hold"},
		{name: "the zero address in an array", value: []netip.Addr{netip.MustParseAddr("10.0.0.1"), zero}, shape: "[4]",
			reason: "the zero netip.Addr holds no address"},
		{name: "an IPv6 address in a tuple", value: []any{netip.MustParseAddr("2001:db8::1"), 1}, shape: "(4,n)",
			reason: "which an IPv4 value cannot hold"},
		{name: "the zero address in a map", value: map[string]netip.Addr{"k": zero}, shape: "{q:6}", reason: "holds no address"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reason := refusal(t, sglClickHouseValue(testCase.value, testCase.shape))
			require.True(t, strings.HasPrefix(reason, "sgl: cannot write the value of a ClickHouse parameter: "), reason)
			require.Contains(t, reason, testCase.reason)
		})
	}
}

func TestClickHouseValueKeepsHostileTextInsideItsElement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value any
		name  string
		shape string
		want  string
	}{
		{name: "a named number with a hostile String method is written as its number", value: []valuesHostileCount{1}, shape: "[n]", want: "[1]"},
		{name: "a hostile String method is quoted and escaped as text", value: []valuesLabel{{text: "x'],evil['"}}, shape: "[q]", want: `['x\'],evil[\'']`},
		{name: "an unknown shape writes a named number as its number", value: []any{valuesHostileCount(2)}, shape: "a", want: "[2]"},
		{name: "a quote and a backslash cannot close the element", value: map[string]string{`\'`: `'\`}, shape: "{q:q}", want: `{'\\\'':'\'\\'}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglClickHouseValue(testCase.value, testCase.shape))
		})
	}
}

func TestClickHouseValueRefusesWhatItCannotWrite(t *testing.T) {
	t.Parallel()

	var cycle any
	cycle = &cycle
	loop := []any{nil}
	loop[0] = loop

	testCases := []struct {
		value  any
		name   string
		shape  string
		reason string
	}{
		{name: "a pointer that refers to itself", value: cycle, shape: "q", reason: "refers to itself"},
		{name: "a slice that holds itself", value: loop, shape: "a", reason: "refers to itself"},
		{name: "a tuple that refers to itself", value: cycle, shape: "(n)", reason: "refers to itself"},
		{name: "an error", value: errors.New("boom"), shape: "q", reason: "an error is not a value"},
		{name: "an error inside an array", value: []error{errors.New("boom")}, shape: "[a]", reason: "an error is not a value"},
		{name: "a failing String method", value: valuesFallible{text: "ignored", err: errors.New("no\ttext")}, shape: "q", reason: "its String method failed: no text"},
		{name: "text where a number belongs", value: "1);DROP", shape: "n", reason: `"1);DROP" is not a number`},
		{name: "a stringer where a number belongs", value: []valuesLabel{{text: "1]"}}, shape: "[n]", reason: `"1]" is not a number`},
		{name: "a struct without a String method as text", value: valuesPoint{X: 1}, shape: "q", reason: "cannot be written as text"},
		{name: "a channel", value: make(chan int), shape: "a", reason: "cannot be written as text"},
		{name: "a string where an array belongs", value: "[1,2]", shape: "[n]", reason: "an array needs a slice or an array"},
		{name: "a slice where a map belongs", value: []int{1}, shape: "{n:n}", reason: "a map needs a Go map"},
		{name: "a tuple of the wrong length", value: []int{1, 2, 3}, shape: "(n,n)", reason: "the tuple has 2 fields but the value holds 3"},
		{name: "a tuple from a struct with unexported fields", value: valuesHidden{visible: 1, Name: "x"}, shape: "(n,q)", reason: "unexported fields"},
		{name: "a tuple from a map without field names", value: map[string]int{"a": 1}, shape: "(n)", reason: "only by field names"},
		{name: "a time where a number belongs", value: time.Unix(0, 0), shape: "n", reason: "cannot be written as a number"},
		{name: "a malformed shape", value: 1, shape: "[n", reason: "malformed ClickHouse parameter shape"},
		{name: "an unknown shape letter", value: 1, shape: "x", reason: "malformed ClickHouse parameter shape"},
		{name: "a shape with text left over", value: 1, shape: "nn", reason: "left over"},
		{name: "a map shape with one element", value: map[int]int{}, shape: "{n}", reason: "malformed ClickHouse parameter shape"},
		{name: "a tuple field name that is not closed", value: 1, shape: "('a", reason: "is not closed"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reason := refusal(t, sglClickHouseValue(testCase.value, testCase.shape))
			require.True(t, strings.HasPrefix(reason, "sgl: "), reason)
			require.Contains(t, reason, testCase.reason)
		})
	}
}

func TestClickHouseValueReadsEveryShapeTheGeneratorWrites(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		shape string
	}{
		{name: "text", shape: "q"},
		{name: "a number", shape: "n"},
		{name: "a date", shape: "d"},
		{name: "a date and time", shape: "t"},
		{name: "a date and time with digits", shape: "t6"},
		{name: "an unknown type", shape: "a"},
		{name: "an array", shape: "[q]"},
		{name: "a nested array", shape: "[[n]]"},
		{name: "a map", shape: "{q:[t3]}"},
		{name: "a tuple", shape: "(n,q,d)"},
		{name: "a named tuple", shape: "('a'=n,'b''c'=[q])"},
		{name: "an IPv4 address", shape: "4"},
		{name: "an address of either version", shape: "6"},
		{name: "a tuple of addresses", shape: "('near'=4,'far'=[6])"},
		{name: "a map of tuples", shape: "{n:('x'=n,'y'={q:n})}"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := sglClickHouseParseShape(testCase.shape)
			require.NoError(t, err)
			require.NotZero(t, parsed.kind)
		})
	}
}

func TestEncodedWritesADocumentAsTheTextOfItsJSONEncoding(t *testing.T) {
	t.Parallel()

	type show struct {
		Title   string `json:"title"`
		Seasons int    `json:"seasons"`
	}
	var missing *show
	var noTags []string
	testCases := []struct {
		value any
		name  string
		want  string
	}{
		{name: "a plain struct", value: show{Title: "Marple", Seasons: 6}, want: `{"title":"Marple","seasons":6}`},
		{name: "a slice of strings", value: []string{"crime", "village"}, want: `["crime","village"]`},
		{name: "a text that escapes", value: map[string]string{"line": "a\tb"}, want: `{"line":"a\\tb"}`},
		{name: "a nil pointer as NULL", value: missing, want: `\N`},
		{name: "a nil slice as NULL", value: noTags, want: `\N`},
		{name: "a value that does not encode", value: make(chan int), want: "unsupported type"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, sglClickHouseValue(sglClickHouseEncoded(testCase.value), "q"), testCase.want)
		})
	}
}

type valuesAmount struct {
	text string
}

func (amount valuesAmount) Value() (driver.Value, error) {
	return amount.text, nil
}

func TestClickHouseNullableHandsTheDriverNilForANilPointer(t *testing.T) {
	t.Parallel()

	var missing *valuesAmount
	amount := &valuesAmount{text: "2.50"}

	testCases := []struct {
		value any
		want  any
		name  string
	}{
		{name: "a nil pointer is nil", value: missing, want: nil},
		{name: "nil is nil", value: nil, want: nil},
		{name: "a decimal is itself", value: amount, want: amount},
		{name: "a decimal held by value is itself", value: valuesAmount{text: "4"}, want: valuesAmount{text: "4"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglClickHouseNullable(testCase.value))
		})
	}
}
