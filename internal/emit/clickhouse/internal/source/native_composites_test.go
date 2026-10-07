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
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type compositesPair struct {
	A uint8  `json:"a"`
	B string `json:"b"`
}

type compositesMood string

type compositesNote []byte

func (note *compositesNote) Scan(source any) error {
	text, isText := source.(string)
	if !isText {
		return errCompositesNote
	}
	*note = compositesNote("note:" + text)
	return nil
}

var errCompositesNote = errors.New("not text")

type compositesRich struct {
	Label     *string                   `json:"label"`
	Missing   *string                   `json:"missing"`
	Counts    []*uint8                  `json:"counts"`
	At        time.Time                 `json:"at"`
	Wide      *big.Int                  `json:"wide"`
	Address   netip.Addr                `json:"address"`
	Mood      compositesMood            `json:"mood"`
	Note      compositesNote            `json:"note"`
	Inner     compositesPair            `json:"inner"`
	Keyed     map[string]compositesPair `json:"keyed"`
	Anything  any                       `json:"anything"`
	Positions []any                     `json:"positions"`
	Tagged    uint16                    `ch:"tagged_by_ch"`
	Named     int8
}

type compositesTarget struct {
	value any
	name  string
}

func TestScanNativeCompositeReadsTheDriversFormsIntoGeneratedTypes(t *testing.T) {
	t.Parallel()

	moment := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	label := "gull"
	var noLabel *string
	one := uint8(1)
	wide := new(big.Int).Lsh(big.NewInt(1), 100)

	t.Run("a map to named tuples", func(t *testing.T) {
		t.Parallel()

		var destination map[string]compositesPair
		source := map[string]map[string]any{"k": {"a": uint8(1), "b": "x"}, "j": {"a": uint8(2), "b": "y"}}
		require.NoError(t, sglScanNativeComposite(&destination).Scan(source))
		require.Equal(t, map[string]compositesPair{"k": {A: 1, B: "x"}, "j": {A: 2, B: "y"}}, destination)
	})

	t.Run("an array of maps to named tuples keeps an empty map and an empty array", func(t *testing.T) {
		t.Parallel()

		var destination []map[uint8]compositesPair
		source := []map[uint8]map[string]any{{7: {"a": uint8(8), "b": "u"}}, {}}
		require.NoError(t, sglScanNativeComposite(&destination).Scan(source))
		require.Equal(t, []map[uint8]compositesPair{{7: {A: 8, B: "u"}}, {}}, destination)

		var empty []map[uint8]compositesPair
		require.NoError(t, sglScanNativeComposite(&empty).Scan([]map[uint8]map[string]any{}))
		require.NotNil(t, empty)
		require.Empty(t, empty)
	})

	t.Run("maps keyed by dates and by an enum's text, to arrays and maps of named tuples", func(t *testing.T) {
		t.Parallel()

		var dated map[time.Time][]compositesPair
		require.NoError(t, sglScanNativeComposite(&dated).Scan(map[time.Time][]map[string]any{moment: {{"a": uint8(5), "b": "v"}}}))
		require.Equal(t, map[time.Time][]compositesPair{moment: {{A: 5, B: "v"}}}, dated)

		var moods map[compositesMood]map[string]compositesPair
		require.NoError(t, sglScanNativeComposite(&moods).Scan(map[string]map[string]map[string]any{"calm": {"o": {"a": uint8(6), "b": "w"}}}))
		require.Equal(t, map[compositesMood]map[string]compositesPair{"calm": {"o": {A: 6, B: "w"}}}, moods)
	})

	t.Run("the driver's values of every element inside a named tuple", func(t *testing.T) {
		t.Parallel()

		var destination map[string]compositesRich
		source := map[string]map[string]any{"k": {
			"label":        &label,
			"missing":      noLabel,
			"counts":       []*uint8{&one, nil},
			"at":           moment,
			"wide":         *wide,
			"address":      net.IPv4(10, 0, 0, 1).To4(),
			"mood":         "calm",
			"note":         "text",
			"inner":        map[string]any{"a": uint8(3), "b": "z"},
			"keyed":        map[string]map[string]any{"q": {"a": uint8(4), "b": "r"}},
			"anything":     "whatever",
			"positions":    []any{uint8(1), "one"},
			"tagged_by_ch": uint16(9),
			"Named":        int8(-3),
			"unmatched":    "ignored",
		}}
		require.NoError(t, sglScanNativeComposite(&destination).Scan(source))
		require.Equal(t, map[string]compositesRich{"k": {
			Label: &label, Missing: nil, Counts: []*uint8{&one, nil}, At: moment, Wide: wide,
			Address: netip.MustParseAddr("10.0.0.1"), Mood: "calm", Note: compositesNote("note:text"),
			Inner: compositesPair{A: 3, B: "z"}, Keyed: map[string]compositesPair{"q": {A: 4, B: "r"}},
			Anything: "whatever", Positions: []any{uint8(1), "one"}, Tagged: 9, Named: -3,
		}}, destination)
	})

	t.Run("an IPv6 address keeps its sixteen bytes", func(t *testing.T) {
		t.Parallel()

		var destination map[string]netip.Addr
		require.NoError(t, sglScanNativeComposite(&destination).Scan(map[string]net.IP{"v6": net.ParseIP("::ffff:10.0.0.1")}))
		require.Equal(t, map[string]netip.Addr{"v6": netip.MustParseAddr("::ffff:10.0.0.1")}, destination)
	})

	t.Run("NULL leaves the destination at its zero value", func(t *testing.T) {
		t.Parallel()

		destination := map[string]compositesPair{"old": {A: 1, B: "x"}}
		require.NoError(t, sglScanNativeComposite(&destination).Scan(nil))
		require.Nil(t, destination)
	})
}

func TestScanNativeCompositeRefusesAValueOfAnotherShape(t *testing.T) {
	t.Parallel()

	testCases := []compositesTarget{
		{name: "a struct from a list", value: []any{uint8(1), "x"}},
		{name: "a struct from a map keyed by numbers", value: map[uint8]any{1: uint8(1)}},
		{name: "a number from text", value: map[string]any{"a": "one", "b": "x"}},
		{name: "a field that refuses its value", value: map[string]any{"a": uint8(1), "b": int64(2)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var destination map[string]compositesPair
			err := sglScanNativeComposite(&destination).Scan(map[string]any{"k": testCase.value})
			require.ErrorContains(t, err, "sgl: read")
		})
	}

	var notes map[string]compositesNote
	require.ErrorIs(t, sglScanNativeComposite(&notes).Scan(map[string]any{"k": uint8(1)}), errCompositesNote)

	var listed []compositesPair
	require.ErrorContains(t, sglScanNativeComposite(&listed).Scan(map[string]any{"a": uint8(1)}), "cannot read")
}

type compositesAddressed struct {
	Address  netip.Addr       `json:"address"`
	Optional *netip.Addr      `json:"optional"`
	Wide     *big.Int         `json:"wide"`
	Listed   []netip.Addr     `json:"listed"`
	Keyed    map[string]uint8 `json:"keyed"`
	Inner    compositesPair   `json:"inner"`
}

func TestReadNativeColumnConvertsTheMapTheDriverFilledForANamedTuple(t *testing.T) {
	t.Parallel()

	optional := net.IP{10, 0, 0, 2}
	source := map[string]any{
		"address":  net.IP{10, 0, 0, 1},
		"optional": &optional,
		"wide":     *big.NewInt(-170),
		"listed":   []net.IP{net.ParseIP("::1"), {192, 168, 0, 1}},
		"keyed":    map[string]uint8{"k": 7},
		"inner":    map[string]any{"a": uint8(3), "b": "tern"},
		"extra":    "ignored",
	}
	var target compositesAddressed
	require.NoError(t, sglReadNativeColumn(&target, source))

	two := netip.MustParseAddr("10.0.0.2")
	require.Equal(t, compositesAddressed{
		Address:  netip.MustParseAddr("10.0.0.1"),
		Optional: &two,
		Wide:     big.NewInt(-170),
		Listed:   []netip.Addr{netip.MustParseAddr("::1"), netip.MustParseAddr("192.168.0.1")},
		Keyed:    map[string]uint8{"k": 7},
		Inner:    compositesPair{A: 3, B: "tern"},
	}, target)

	var none compositesAddressed
	require.NoError(t, sglReadNativeColumn(&none, map[string]any{"optional": (*net.IP)(nil)}))
	require.Nil(t, none.Optional)

	require.ErrorContains(t, sglReadNativeColumn(&none, map[string]any{"inner": "not a tuple"}), "sgl: read map[string]interface {} into source.compositesAddressed")
}

type (
	overrideLabel string
	overrideCount int64
	overrideRatio float64
	overrideFlag  bool
	overrideBlob  []byte
)

func TestScanNativeCompositeReadsTheDriversValuesIntoOverrideTypes(t *testing.T) {
	t.Parallel()

	label := overrideLabel("gull")
	testCases := []struct {
		name   string
		source any
		read   func(source any) (any, error)
		want   any
	}{
		{name: "a string into a named string", source: "gull", read: scanInto[overrideLabel], want: overrideLabel("gull")},
		{name: "a string into a named byte slice", source: "gull", read: scanInto[overrideBlob], want: overrideBlob("gull")},
		{name: "a string into json.RawMessage", source: `{"n":1}`, read: scanInto[json.RawMessage], want: json.RawMessage(`{"n":1}`)},
		{name: "an integer into a named integer", source: int64(7), read: scanInto[overrideCount], want: overrideCount(7)},
		{name: "an integer into a narrower integer", source: int64(7), read: scanInto[int32], want: int32(7)},
		{name: "a float into a named float", source: 2.5, read: scanInto[overrideRatio], want: overrideRatio(2.5)},
		{name: "a float into a narrower float", source: 2.5, read: scanInto[float32], want: float32(2.5)},
		{name: "a boolean into a named boolean", source: true, read: scanInto[overrideFlag], want: overrideFlag(true)},
		{name: "an address into a named string", source: net.IP{10, 0, 0, 1}, read: scanInto[json.Number], want: json.Number("10.0.0.1")},
		{name: "an address into netip.Addr", source: net.IP{10, 0, 0, 1}, read: scanInto[netip.Addr], want: netip.MustParseAddr("10.0.0.1")},
		{name: "text into a type that reads text", source: "10.0.0.1", read: scanInto[netip.Addr], want: netip.MustParseAddr("10.0.0.1")},
		{name: "a string into a pointer to a named string", source: "gull", read: scanInto[*overrideLabel], want: &label},
		{name: "NULL into a pointer to a named string", source: nil, read: scanInto[*overrideLabel], want: (*overrideLabel)(nil)},
		{name: "a list of strings into a list of named strings", source: []string{"a", "b"}, read: scanInto[[]overrideLabel], want: []overrideLabel{"a", "b"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := testCase.read(testCase.source)
			require.NoError(t, err)
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestScanNativeCompositeRefusesAnOverrideValueItCannotHold(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source any
		read   func(source any) (any, error)
	}{
		{name: "an integer too wide for the target", source: int64(math.MaxInt32) + 1, read: scanInto[int32]},
		{name: "a negative integer into an unsigned one", source: int64(-1), read: scanInto[uint64]},
		{name: "text into a number", source: "seven", read: scanInto[overrideCount]},
		{name: "a number into a boolean", source: int64(1), read: scanInto[overrideFlag]},
		{name: "text that is not an address", source: "gull", read: scanInto[netip.Addr]},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := testCase.read(testCase.source)
			require.ErrorContains(t, err, "sgl: read")
		})
	}
}

func scanInto[T any](source any) (any, error) {
	var destination T
	err := sglScanNativeComposite(&destination).Scan(source)
	return destination, err
}

func TestScanNativeCompositeTakesADocumentTheDriverDeserialisesIntoIt(t *testing.T) {
	t.Parallel()

	var read sglNativeDocument
	scanner := sglScanNativeComposite(&read)
	deserialiser, deserialises := scanner.(interface {
		DeserializeClickHouseJSON(document *sglNativeDocument) error
	})
	require.True(t, deserialises, "the driver hands a JSON column only to a chcol.JSONDeserializer")

	require.NoError(t, deserialiser.DeserializeClickHouseJSON(&sglNativeDocument{}))
	require.NoError(t, scanner.Scan(nil), "a NULL of a Nullable(JSON) column reaches Scan")
}

func TestNativeEntriesTakeTheEntriesOfAMapOneByOne(t *testing.T) {
	t.Parallel()

	var entries sglNativeEntries
	entries.Put("first", int64(1))
	entries.Put("second", int64(2))

	value, found := entries.Get("first")
	require.True(t, found)
	require.Equal(t, int64(1), value)
	_, found = entries.Get("third")
	require.False(t, found)
	var keys []any
	for key := range entries.Keys() {
		keys = append(keys, key)
	}
	require.ElementsMatch(t, []any{"first", "second"}, keys)

	var converted map[string]int32
	require.NoError(t, sglReadNativeColumn(&converted, entries))
	require.Equal(t, map[string]int32{"first": 1, "second": 2}, converted)
}

func TestNativeEntriesReadAsAnEmptyMapWhenTheDriverPutsNone(t *testing.T) {
	t.Parallel()

	var entries sglNativeEntries
	var converted map[string]int32

	require.NoError(t, sglReadNativeColumn(&converted, entries))
	require.Equal(t, map[string]int32{}, converted, "a Map column is never NULL, so an empty one reads as the driver reads any empty Map")
}
