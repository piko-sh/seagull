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
	"math/big"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type appendingPair struct {
	IP netip.Addr `json:"ip"`
	X  uint8      `json:"x"`
}

type appendingHolder struct {
	M map[string]netip.Addr `json:"m"`
	A uint8                 `json:"a"`
}

type appendingLink struct {
	Near netip.Addr  `json:"near"`
	Far  *netip.Addr `json:"far"`
}

type appendingNamed struct {
	Wide     *big.Int `json:"wide,omitempty"`
	Tagged   uint8    `ch:"tagged_by_ch"`
	Plain    string
	Skipped  string `json:"-"`
	Anything any    `json:"anything"`
	hidden   uint8
}

type appendingStamp struct {
	At time.Time
}

func (stamp appendingStamp) Value() (driver.Value, error) {
	return stamp.At, nil
}

func appendingShape(t *testing.T, text string) sglClickHouseShape {
	t.Helper()
	shape, err := sglClickHouseParseShape(text)
	require.NoError(t, err)
	return shape
}

func TestAppendNativeRowAppendsEachValueCheckedAndConverted(t *testing.T) {
	t.Parallel()

	plain := netip.MustParseAddr("10.0.0.1")
	mapped := netip.MustParseAddr("::ffff:10.0.0.9")
	wide := netip.MustParseAddr("2001:db8::1")
	link := appendingLink{Near: mapped, Far: &wide}
	aliases := map[string]netip.Addr{"a": plain, "m": mapped}

	var appended []any
	err := sglAppendNativeRow(func(values ...any) error {
		appended = values
		return nil
	}, "('id'=a,'host'=4,'peer'=6,'link'=('near'=4,'far'=6),'aliases'={q:4},'mirrors'={q:6})",
		uint64(7), mapped, plain, link, aliases, map[string]netip.Addr{"m": mapped})

	require.NoError(t, err)
	require.Equal(t, []any{uint64(7), mapped, plain, link,
		map[string]net.IP{"a": net.IP(plain.AsSlice()), "m": net.IP{10, 0, 0, 9}},
		map[string]net.IP{"m": net.IP(mapped.AsSlice())}}, appended)
}

func TestAppendNativeRowRefusesAnAddressTheValueCannotHoldBeforeAppending(t *testing.T) {
	t.Parallel()

	zero := netip.Addr{}
	wide := netip.MustParseAddr("2001:db8::1")
	plain := netip.MustParseAddr("10.0.0.1")
	var empty net.IP

	testCases := []struct {
		value  any
		name   string
		shape  string
		reason string
	}{
		{name: "the zero address for IPv4", shape: "4", value: zero, reason: "the zero netip.Addr holds no address"},
		{name: "the zero address for IPv6", shape: "6", value: zero, reason: "the zero netip.Addr holds no address"},
		{name: "an IPv6 address for IPv4", shape: "4", value: wide, reason: "2001:db8::1 is an IPv6 address, which an IPv4 value cannot hold"},
		{name: "a pointer to the zero address for a nullable", shape: "4", value: &zero, reason: "holds no address"},
		{name: "an IPv6 address in an array", shape: "[4]", value: []netip.Addr{plain, wide}, reason: "which an IPv4 value cannot hold"},
		{name: "the zero address in an array of nullables", shape: "[4]", value: []*netip.Addr{nil, &zero}, reason: "holds no address"},
		{name: "an IPv6 address in a named tuple", shape: "('near'=4,'far'=6)", value: appendingLink{Near: wide, Far: &wide},
			reason: "which an IPv4 value cannot hold"},
		{name: "the zero address in a named tuple's nullable", shape: "('near'=4,'far'=6)", value: appendingLink{Near: plain, Far: &zero},
			reason: "holds no address"},
		{name: "an IPv6 address in an unnamed tuple", shape: "('_1'=4,'_2'=n)", value: []any{wide, uint8(1)},
			reason: "which an IPv4 value cannot hold"},
		{name: "an IPv6 address in a map", shape: "{q:4}", value: map[string]netip.Addr{"k": wide}, reason: "which an IPv4 value cannot hold"},
		{name: "the zero address in a map of nullables", shape: "{q:4}", value: map[string]*netip.Addr{"k": &zero}, reason: "holds no address"},
		{name: "the zero address in a map for IPv6", shape: "{q:6}", value: map[string]netip.Addr{"k": zero}, reason: "holds no address"},
		{name: "the zero address in a named tuple of a map", shape: "{q:('ip'=4,'x'=n)}",
			value: map[string]appendingPair{"k": {IP: zero, X: 1}}, reason: "holds no address"},
		{name: "an empty net.IP", shape: "4", value: empty, reason: "an empty net.IP holds no address"},
		{name: "a net.IP of the wrong length", shape: "6", value: net.IP{1, 2, 3}, reason: "a net.IP of 3 bytes is not an IP address"},
		{name: "a sixteen-byte IPv6 net.IP for IPv4", shape: "4", value: net.ParseIP("2001:db8::2"), reason: "which an IPv4 value cannot hold"},
		{name: "text that is no address", shape: "4", value: "10.0.0", reason: `"10.0.0" is not an IP address`},
		{name: "text holding an IPv6 address for IPv4", shape: "4", value: "2001:db8::3", reason: "which an IPv4 value cannot hold"},
		{name: "the zero address where the shape says nothing", shape: "('_1'=4)", value: []any{plain, zero}, reason: "holds no address"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			called := false
			err := sglAppendNativeRow(func(...any) error {
				called = true
				return nil
			}, "('id'=n,'host'="+testCase.shape+")", uint64(1), testCase.value)

			require.ErrorIs(t, err, errSglClickHouseAppend)
			require.ErrorContains(t, err, "sgl: cannot append the value of ClickHouse parameter host: ")
			require.ErrorContains(t, err, testCase.reason)
			require.False(t, called, "a refused row is not appended")
		})
	}
}

func TestAppendNativeRowAppendsNullAndValidAddressesOfEveryForm(t *testing.T) {
	t.Parallel()

	mapped := netip.MustParseAddr("::ffff:10.0.0.9")
	var missing *netip.Addr

	testCases := []struct {
		value any
		name  string
		shape string
	}{
		{name: "a nil pointer is NULL", shape: "4", value: missing},
		{name: "nothing is NULL", shape: "6", value: nil},
		{name: "an IPv4-mapped address for IPv4, which the driver appends as IPv4", shape: "4", value: mapped},
		{name: "an IPv4 address for IPv6", shape: "6", value: netip.MustParseAddr("10.0.0.1")},
		{name: "a zoned address for IPv6", shape: "6", value: netip.MustParseAddr("fe80::1%eth0")},
		{name: "a four-byte net.IP", shape: "4", value: net.IP{10, 0, 0, 1}},
		{name: "a sixteen-byte IPv4 net.IP for IPv4", shape: "4", value: net.ParseIP("10.0.0.2")},
		{name: "text holding an IPv4-mapped address for IPv4", shape: "4", value: "::ffff:10.0.0.3"},
		{name: "an array of nullable addresses", shape: "[4]", value: []*netip.Addr{&mapped, nil}},
		{name: "a value whose shape is appended as it is", shape: "a", value: netip.Addr{}},
		{name: "a number", shape: "n", value: uint8(4)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var appended []any
			err := sglAppendNativeRow(func(values ...any) error {
				appended = values
				return nil
			}, "('id'=n,'host'="+testCase.shape+")", uint64(1), testCase.value)

			require.NoError(t, err)
			require.Equal(t, []any{uint64(1), testCase.value}, appended)
		})
	}
}

func TestAppendNativeRowRefusesARowItsShapeDoesNotDescribe(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		shape  string
		values []any
	}{
		{name: "a malformed shape", shape: "('id'=n", values: []any{1}},
		{name: "a shape that is not a row", shape: "[n]", values: []any{1}},
		{name: "a row of fewer values", shape: "('id'=n,'host'=4)", values: []any{1}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := sglAppendNativeRow(func(...any) error { return nil }, testCase.shape, testCase.values...)

			require.ErrorIs(t, err, errSglClickHouseShape)
		})
	}
}

func TestAppendNativeRowReturnsWhatTheBatchReturns(t *testing.T) {
	t.Parallel()

	failure := errors.New("the batch failed")

	err := sglAppendNativeRow(func(...any) error { return failure }, "('host'=4)", netip.MustParseAddr("10.0.0.1"))

	require.ErrorIs(t, err, failure)
}

func TestAppendNativeValueConvertsWhatAMapColumnTakesOnlyInTheDriversTypes(t *testing.T) {
	t.Parallel()

	first := netip.MustParseAddr("10.0.0.1")
	second := netip.MustParseAddr("2001:db8::1")
	mapped := netip.MustParseAddr("::ffff:10.0.0.9")
	firstIP := net.IP{10, 0, 0, 1}
	secondIP := net.IP(second.AsSlice())
	mappedIP := net.IP(mapped.AsSlice())
	moment := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)

	testCases := []struct {
		value any
		want  any
		name  string
		shape string
	}{
		{name: "nothing", shape: "{q:4}", value: nil, want: nil},
		{name: "a map of addresses of either version, each in its own form", shape: "{q:6}",
			value: map[string]netip.Addr{"a": first, "b": second, "c": mapped}, want: map[string]net.IP{"a": firstIP, "b": secondIP, "c": mappedIP}},
		{name: "a map of IPv4 addresses, an IPv4-mapped one as IPv4", shape: "{q:4}",
			value: map[string]netip.Addr{"a": first, "c": mapped}, want: map[string]net.IP{"a": firstIP, "c": {10, 0, 0, 9}}},
		{name: "a map of nullable addresses", shape: "{q:4}", value: map[string]*netip.Addr{"set": &first, "unset": nil},
			want: map[string]*net.IP{"set": &firstIP, "unset": nil}},
		{name: "a map of lists of addresses", shape: "{q:[6]}", value: map[string][]netip.Addr{"two": {first, second}, "empty": {}, "none": nil},
			want: map[string][]net.IP{"two": {firstIP, secondIP}, "empty": {}, "none": nil}},
		{name: "a map of lists of nullable addresses", shape: "{q:[6]}", value: map[string][]*netip.Addr{"mixed": {&second, nil}},
			want: map[string][]*net.IP{"mixed": {&secondIP, nil}}},
		{name: "a map of maps of addresses", shape: "{q:{q:6}}", value: map[string]map[string]netip.Addr{"outer": {"inner": second}, "none": nil},
			want: map[string]map[string]net.IP{"outer": {"inner": secondIP}, "none": nil}},
		{name: "a list of maps of addresses", shape: "[{q:4}]", value: []map[string]netip.Addr{{"k": first}, {}},
			want: []map[string]net.IP{{"k": firstIP}, {}}},
		{name: "a nil map of addresses", shape: "{q:4}", value: map[string]netip.Addr(nil), want: map[string]net.IP(nil)},
		{name: "a named tuple holding a map of addresses", shape: "('m'={q:4},'a'=n)",
			value: appendingHolder{A: 7, M: map[string]netip.Addr{"held": first}},
			want:  map[string]any{"a": uint8(7), "m": map[string]net.IP{"held": firstIP}}},
		{name: "a map of named tuples", shape: "{q:('ip'=4,'x'=n)}", value: map[string]appendingPair{"p": {X: 3, IP: first}},
			want: map[string]map[string]any{"p": {"x": uint8(3), "ip": firstIP}}},
		{name: "a map of lists of named tuples", shape: "{q:[('ip'=6,'x'=n)]}",
			value: map[string][]appendingPair{"l": {{X: 4, IP: second}}, "none": nil},
			want:  map[string][]map[string]any{"l": {{"x": uint8(4), "ip": secondIP}}, "none": nil}},
		{name: "a map of unnamed tuples holding an address", shape: "{q:('_1'=n,'_2'=4,'_3'=n)}",
			value: map[string][]any{"u": {uint8(6), first, nil}}, want: map[string][]any{"u": {uint8(6), firstIP, nil}}},
		{name: "an unnamed tuple holding a map of addresses", shape: "('_1'=n,'_2'={q:6})", value: []any{uint8(1), map[string]netip.Addr{"k": mapped}},
			want: []any{uint8(1), map[string]net.IP{"k": mappedIP}}},
		{name: "a pointer to a named tuple holding a map", shape: "('m'={q:4},'a'=n)",
			value: &appendingHolder{A: 5, M: map[string]netip.Addr{"k": first}},
			want:  &map[string]any{"a": uint8(5), "m": map[string]net.IP{"k": firstIP}}},
		{name: "a named tuple's fields by json, ch and Go name, leaving out unexported and omitted ones", shape: "{q:('w'=n,'t'=n,'p'=q,'a'=6)}",
			value: map[string]appendingNamed{"k": {Wide: big.NewInt(9), Tagged: 2, Plain: "plain", Skipped: "skipped", Anything: first, hidden: 1}},
			want:  map[string]map[string]any{"k": {"wide": big.NewInt(9), "tagged_by_ch": uint8(2), "Plain": "plain", "anything": firstIP}}},
		{name: "a named tuple's empty interface field", shape: "{q:('w'=n,'t'=n,'p'=q,'a'=6)}", value: map[string]appendingNamed{"k": {}},
			want: map[string]map[string]any{"k": {"wide": (*big.Int)(nil), "tagged_by_ch": uint8(0), "Plain": "", "anything": nil}}},
		{name: "a map of times and stamps that convert themselves", shape: "{q:('at'=t)}",
			value: map[string]any{"at": moment, "stamp": appendingStamp{At: moment}}, want: map[string]any{"at": moment, "stamp": appendingStamp{At: moment}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted, failure := sglAppendNativeValue(testCase.value, appendingShape(t, testCase.shape))
			require.Empty(t, failure)
			require.Equal(t, testCase.want, converted)
		})
	}
}

func TestAppendNativeValueReturnsAValueHoldingNothingToConvertAsItIs(t *testing.T) {
	t.Parallel()

	first := netip.MustParseAddr("10.0.0.1")
	testCases := []struct {
		value any
		name  string
		shape string
	}{
		{name: "a map of text", shape: "{q:q}", value: map[string]string{"k": "v"}},
		{name: "a map of wide integers", shape: "{q:n}", value: map[string]*big.Int{"k": big.NewInt(1), "n": nil}},
		{name: "a map keyed by wide integers", shape: "{n:q}", value: map[*big.Int]string{big.NewInt(2): "v"}},
		{name: "a map of lists of wide integers", shape: "{q:[n]}", value: map[string][]*big.Int{"k": {big.NewInt(3)}}},
		{name: "a map of times", shape: "{q:t}", value: map[string]time.Time{"k": time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)}},
		{name: "a map of unnamed tuples of plain values", shape: "{q:('_1'=n,'_2'=q)}", value: map[string][]any{"k": {uint8(1), "one"}}},
		{name: "a map of IPs already in the driver's form", shape: "{q:4}", value: map[string]net.IP{"k": {10, 0, 0, 1}}},
		{name: "a list of numbers", shape: "[n]", value: []uint8{1, 2, 3}},
		{name: "a list of addresses, which the driver appends itself", shape: "[4]", value: []netip.Addr{first}},
		{name: "a named tuple holding addresses, which the driver appends itself", shape: "('near'=4,'far'=6)",
			value: appendingLink{Near: first, Far: &first}},
		{name: "a map of addresses whose shape is appended as it is", shape: "a", value: map[string]netip.Addr{"k": first}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted, failure := sglAppendNativeValue(testCase.value, appendingShape(t, testCase.shape))
			require.Empty(t, failure)
			require.Equal(t, testCase.value, converted)
			if reflect.TypeOf(testCase.value).Kind() != reflect.Struct {
				require.Equal(t, reflect.ValueOf(testCase.value).UnsafePointer(), reflect.ValueOf(converted).UnsafePointer(),
					"a value holding nothing to convert is not copied")
			}
		})
	}
}

func TestAppendNativeValueRefusesAValueThatRefersToItself(t *testing.T) {
	t.Parallel()

	loop := map[string]any{}
	loop["self"] = loop

	_, failure := sglAppendNativeValue(map[string]any{"k": loop}, appendingShape(t, "{q:('x'=4)}"))

	require.Contains(t, failure, "refers to itself")
}

func TestAppendNativeTypeIsTheTypeOfEveryConvertedValue(t *testing.T) {
	t.Parallel()

	first := netip.MustParseAddr("10.0.0.1")
	testCases := []struct {
		value any
		name  string
		shape string
	}{
		{name: "a map of addresses", shape: "{q:4}", value: map[string]netip.Addr{"k": first}},
		{name: "a map of nullable addresses", shape: "{q:4}", value: map[string]*netip.Addr{"k": nil}},
		{name: "a map of lists of named tuples", shape: "{q:[('ip'=4,'x'=n)]}", value: map[string][]appendingPair{"k": {{X: 1, IP: first}}}},
		{name: "a list of maps of addresses", shape: "[{q:4}]", value: []map[string]netip.Addr{nil}},
		{name: "a pointer to a map of addresses", shape: "{q:4}", value: &map[string]netip.Addr{"k": first}},
		{name: "a nil pointer to a named tuple holding a map", shape: "('m'={q:4},'a'=n)", value: (*appendingHolder)(nil)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted, failure := sglAppendNativeValue(testCase.value, appendingShape(t, testCase.shape))
			require.Empty(t, failure)
			require.Equal(t, sglAppendNativeType(reflect.TypeOf(testCase.value)), reflect.TypeOf(converted))
		})
	}
}

type (
	appendingLabel  string
	appendingCount  int64
	appendingRatio  float64
	appendingFlag   bool
	appendingBlob   []byte
	appendingNumber string
)

func (number appendingNumber) String() string {
	return string(number)
}

type appendingRanked struct {
	Name appendingLabel `json:"name"`
	Rank appendingCount `json:"rank"`
}

type appendingPlain struct {
	Name string `json:"name"`
	Rank int64  `json:"rank"`
}

type appendingValued string

func (valued appendingValued) Value() (driver.Value, error) {
	return "valued:" + string(valued), nil
}

func TestAppendNativeRowAppendsOverrideTypesAsTheDriversOwn(t *testing.T) {
	t.Parallel()

	label := appendingLabel("gull")
	count := appendingCount(7)
	host := appendingNumber("10.0.0.1")
	testCases := []struct {
		value any
		want  any
		name  string
		shape string
	}{
		{name: "a named string", shape: "a", value: label, want: "gull"},
		{name: "a named integer", shape: "a", value: count, want: int64(7)},
		{name: "a named float", shape: "a", value: appendingRatio(2.5), want: 2.5},
		{name: "a named boolean", shape: "a", value: appendingFlag(true), want: true},
		{name: "a named byte slice", shape: "a", value: appendingBlob("raw"), want: []byte("raw")},
		{name: "a pointer to a named string", shape: "a", value: &label, want: new("gull")},
		{name: "a nil pointer to a named integer", shape: "a", value: (*appendingCount)(nil), want: (*int64)(nil)},
		{name: "a list of named strings", shape: "[q]", value: []appendingLabel{"a", "b"}, want: []string{"a", "b"}},
		{name: "a map of named integers", shape: "{q:n}", value: map[string]appendingCount{"k": 1}, want: map[string]int64{"k": 1}},
		{name: "a named string for an address", shape: "4", value: host, want: "10.0.0.1"},
		{name: "a pointer to a named string for a nullable address", shape: "4", value: &host, want: new("10.0.0.1")},
		{name: "a value that converts itself", shape: "a", value: appendingValued("x"), want: appendingValued("x")},
		{name: "an address the driver appends itself", shape: "a", value: net.IP{10, 0, 0, 1}, want: net.IP{10, 0, 0, 1}},
		{name: "json.RawMessage", shape: "q", value: []byte(`{"n":1}`), want: []byte(`{"n":1}`)},
		{name: "a map keyed by a named string", shape: "{q:n}", value: map[appendingLabel]appendingCount{"k": 1}, want: map[string]int64{"k": 1}},
		{name: "a named tuple holding named kinds", shape: "('name'=q,'rank'=n)", value: appendingRanked{Name: "gull", Rank: 3},
			want: map[string]any{"name": "gull", "rank": int64(3)}},
		{name: "a pointer to a named tuple holding named kinds", shape: "('name'=q,'rank'=n)", value: &appendingRanked{Name: "gull", Rank: 3},
			want: &map[string]any{"name": "gull", "rank": int64(3)}},
		{name: "a list of named tuples holding named kinds", shape: "[('name'=q,'rank'=n)]", value: []appendingRanked{{Name: "gull", Rank: 3}},
			want: []map[string]any{{"name": "gull", "rank": int64(3)}}},
		{name: "a named tuple of plain values", shape: "('name'=q,'rank'=n)", value: appendingPlain{Name: "gull", Rank: 3},
			want: appendingPlain{Name: "gull", Rank: 3}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var appended []any
			err := sglAppendNativeRow(func(values ...any) error {
				appended = values
				return nil
			}, "('value'="+testCase.shape+")", testCase.value)
			require.NoError(t, err)
			require.Equal(t, []any{testCase.want}, appended)
		})
	}
}

func TestAppendNativeRowRefusesANamedStringThatIsNoAddress(t *testing.T) {
	t.Parallel()

	err := sglAppendNativeRow(func(...any) error { return nil }, "('host'=4)", appendingNumber("gull"))

	require.ErrorIs(t, err, errSglClickHouseAppend)
	require.ErrorContains(t, err, `"gull" is not an IP address`)
}
