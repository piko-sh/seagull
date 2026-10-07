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
	"context"
	"database/sql/driver"
	"encoding/asn1"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

var errCompositeRefused = errors.New("the composite refused to convert")

type compositeValuer struct {
	text string
}

func (valuer compositeValuer) Value() (driver.Value, error) {
	return "valued " + valuer.text, nil
}

type compositeFailingValuer struct {
	text string
}

func (compositeFailingValuer) Value() (driver.Value, error) {
	return nil, errCompositeRefused
}

func TestDriverCompositeConvertsAParameterIntoTheFormsTheDriverBinds(t *testing.T) {
	t.Parallel()

	moment := time.Date(2024, 3, 5, 10, 11, 12, 0, time.UTC)
	length := 12
	var missing *int

	testCases := []struct {
		value any
		want  any
		name  string
	}{
		{name: "nil binds as NULL", value: nil, want: nil},
		{name: "a nil pointer binds as NULL", value: missing, want: nil},
		{name: "a nil map binds as NULL", value: map[string]int(nil), want: nil},
		{name: "a nil slice binds as NULL", value: []string(nil), want: nil},
		{name: "a scalar passes through", value: 5, want: 5},
		{name: "a string passes through", value: "anchor", want: "anchor"},
		{name: "a pointer is followed to its value", value: &length, want: 12},
		{name: "a map becomes a driver map", value: map[string]int{"a": 1, "b": 2}, want: sglDriverMap{"a": 1, "b": 2}},
		{name: "a map keeps keys of any type", value: map[int]string{1: "one"}, want: sglDriverMap{1: "one"}},
		{name: "a map converts its values", value: map[string][]int{"a": {1, 2}}, want: sglDriverMap{"a": []any{1, 2}}},
		{name: "a map holds a nil pointer value as nil", value: map[string]*int{"a": nil}, want: sglDriverMap{"a": nil}},
		{name: "a slice becomes a list of values", value: []int{1, 2}, want: []any{1, 2}},
		{name: "a nested slice becomes nested lists", value: [][]string{{"a"}, {"b", "c"}}, want: []any{[]any{"a"}, []any{"b", "c"}}},
		{name: "a byte slice passes through", value: []byte("raw"), want: []byte("raw")},
		{name: "a named byte slice binds as bytes", value: json.RawMessage(`{}`), want: []byte(`{}`)},
		{
			name: "a struct becomes a map keyed by its fields' json names",
			value: struct {
				Pontoon string `json:"pontoon"`
				Number  int    `json:"number,omitempty"`
				Secret  string `json:"-"`
				Plain   bool
			}{Pontoon: "B", Number: 7, Secret: "hidden", Plain: true},
			want: map[string]any{"pontoon": "B", "number": 7, "Plain": true},
		},
		{
			name:  "a pointer to a struct converts the struct",
			value: &struct{ Number int }{Number: 3},
			want:  map[string]any{"Number": 3},
		},
		{
			name: "a struct converts its nested struct fields",
			value: struct {
				Berth struct {
					Number int `json:"number"`
				} `json:"berth"`
			}{Berth: struct {
				Number int `json:"number"`
			}{Number: 4}},
			want: map[string]any{"berth": map[string]any{"number": 4}},
		},
		{
			name: "a slice of structs becomes a list of maps",
			value: []struct {
				Number int `json:"number"`
			}{{Number: 1}, {Number: 2}},
			want: []any{map[string]any{"number": 1}, map[string]any{"number": 2}},
		},
		{name: "a time passes through", value: moment, want: moment},
		{name: "a valuer is converted through its value", value: compositeValuer{text: "cleat"}, want: "valued cleat"},
		{name: "a valuer inside a slice is converted", value: []compositeValuer{{text: "a"}}, want: []any{"valued a"}},
		{name: "a driver map passes through untouched", value: sglDriverMap{"k": []int{1}}, want: sglDriverMap{"k": []int{1}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglDriverComposite(testCase.value))
		})
	}
}

func TestDriverCompositeReportsTheErrorOfAValuerThatFails(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value any
		name  string
	}{
		{name: "a valuer that fails", value: compositeFailingValuer{text: "keel"}},
		{name: "a pointer to a valuer that fails", value: &compositeFailingValuer{text: "keel"}},
		{name: "a valuer that fails inside a slice", value: []any{1, compositeFailingValuer{text: "keel"}}},
		{name: "a valuer that fails inside a map", value: map[string]any{"a": compositeFailingValuer{text: "keel"}}},
		{name: "a valuer that fails inside a struct", value: struct{ Keel compositeFailingValuer }{Keel: compositeFailingValuer{text: "keel"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted := sglDriverComposite(testCase.value)
			valuer, ok := converted.(driver.Valuer)
			require.True(t, ok, "the conversion is refused through a driver.Valuer, got %#v", converted)
			_, err := valuer.Value()
			require.ErrorIs(t, err, errCompositeRefused)
			_, err = driver.DefaultParameterConverter.ConvertValue(converted)
			require.ErrorIs(t, err, errCompositeRefused, "database/sql reports the error when it converts the argument")
		})
	}
}

func TestDriverInferredListHandsTheDriverAListItBindsByItsElements(t *testing.T) {
	t.Parallel()

	numbers := []json.Number{"1.25", "7"}
	var missing *[]json.Number

	testCases := []struct {
		value any
		want  any
		name  string
	}{
		{name: "nil binds as NULL", value: nil, want: nil},
		{name: "a nil slice binds as NULL", value: []json.Number(nil), want: nil},
		{name: "a nil pointer binds as NULL", value: missing, want: nil},
		{name: "an empty slice is an empty list", value: []json.Number{}, want: sglDriverList{}},
		{name: "defined strings become strings", value: numbers, want: sglDriverList{"1.25", "7"}},
		{name: "a pointer is followed to its slice", value: &numbers, want: sglDriverList{"1.25", "7"}},
		{name: "a valuer element becomes the value it writes", value: []compositeValuer{{text: "a"}}, want: sglDriverList{"valued a"}},
		{name: "a nested slice becomes nested lists", value: [][]json.Number{{"1"}, {}}, want: sglDriverList{[]any{"1"}, []any{}}},
		{name: "byte slices inside the list stay bytes", value: [][]byte{[]byte("ab")}, want: sglDriverList{[]byte("ab")}},
		{name: "the outer byte slice is a list of numbers", value: []uint8{1, 2}, want: sglDriverList{uint8(1), uint8(2)}},
		{name: "a valuer of the whole list binds as its value", value: compositeValuer{text: "list"}, want: "valued list"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglDriverInferredList(testCase.value))
		})
	}
}

func TestDriverListGivesItsElementsAsItsValue(t *testing.T) {
	t.Parallel()

	value, err := sglDriverList{"calm", uint8(1)}.Value()

	require.NoError(t, err)
	require.Equal(t, []any{"calm", uint8(1)}, value)
}

func TestDriverInferredListReportsTheErrorOfAnElementThatFails(t *testing.T) {
	t.Parallel()

	converted := sglDriverInferredList([]any{"calm", compositeFailingValuer{text: "keel"}})

	valuer, ok := converted.(driver.Valuer)
	require.True(t, ok, "the list is refused through a driver.Valuer, got %#v", converted)
	_, err := valuer.Value()
	require.ErrorIs(t, err, errCompositeRefused)
}

type compositeLoop *compositeLoop

func nestedSlices(levels int) any {
	var value any = "anchor"
	for range levels {
		value = []any{value}
	}
	return value
}

func selfReferringValues() map[string]any {
	looped := map[string]any{"name": "looped"}
	looped["self"] = looped

	holding := []any{"first", nil}
	holding[1] = holding

	node := &struct {
		Next  any    `json:"next"`
		Label string `json:"label"`
	}{Next: nil, Label: "head"}
	node.Next = node

	var loop compositeLoop
	loop = &loop

	return map[string]any{
		"a map holding itself":                  looped,
		"a slice holding itself":                holding,
		"a struct whose field points at itself": node,
		"a pointer pointing at itself":          loop,
		"a value nesting past the limit":        nestedSlices(sglDriverMaxDepth + 1),
	}
}

func TestDriverConversionRefusesAValueThatRefersToItselfOrNestsTooDeep(t *testing.T) {
	t.Parallel()

	adapters := []struct {
		adapt func(any) any
		name  string
	}{
		{name: "the composite conversion", adapt: sglDriverComposite},
		{name: "the inferred list", adapt: sglDriverInferredList},
	}

	for _, adapter := range adapters {
		for name, value := range selfReferringValues() {
			t.Run(adapter.name+"/"+name, func(t *testing.T) {
				t.Parallel()

				scaling.Within(t, time.Second, func(context.Context) {
					converted := adapter.adapt(value)

					valuer, ok := converted.(driver.Valuer)
					require.True(t, ok, "the parameter is refused through a driver.Valuer, got %T", converted)
					_, err := valuer.Value()
					require.ErrorContains(t, err, "nests more than 32 levels deep or refers to itself")
				})
			})
		}
	}
}

func TestDriverConversionConvertsAValueNestedUpToTheLimit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		adapt func(any) any
		name  string
	}{
		{name: "the composite conversion", adapt: sglDriverComposite},
		{name: "the inferred list", adapt: sglDriverInferredList},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted := testCase.adapt(nestedSlices(sglDriverMaxDepth))

			_, refused := converted.(sglDriverRefusal)
			require.False(t, refused, "a value nesting exactly as deep as the limit converts")
		})
	}
}

func TestDriverCompositePassesAStructWithAnUnexportedFieldThroughUnchanged(t *testing.T) {
	t.Parallel()

	value := struct {
		Visible int
		hidden  int
	}{Visible: 1, hidden: 2}

	require.Equal(t, value, sglDriverComposite(value))
}

var (
	errIdentifierLength = errors.New("an identifier holds four bytes")
	errDocumentText     = errors.New("a document scans only from text")
)

type scannedOrderedMap struct {
	keys   []any
	values []any
}

func (ordered *scannedOrderedMap) Keys() []any {
	return ordered.keys
}

func (ordered *scannedOrderedMap) Values() []any {
	return ordered.values
}

type scannedExact struct {
	text string
}

func (exact *scannedExact) UnmarshalText(text []byte) error {
	exact.text = string(text)
	return nil
}

type scannedIdentifier [4]byte

func (identifier *scannedIdentifier) Scan(source any) error {
	raw, ok := source.([]byte)
	if !ok || len(raw) != len(identifier) {
		return errIdentifierLength
	}
	copy(identifier[:], raw)
	return nil
}

type scannedDocument []byte

func (document *scannedDocument) UnmarshalJSON(data []byte) error {
	*document = append(scannedDocument(nil), data...)
	return nil
}

func (document *scannedDocument) Scan(source any) error {
	text, ok := source.(string)
	if !ok {
		return errDocumentText
	}
	*document = scannedDocument(text)
	return nil
}

type scannedMood string

type scannedStop struct {
	At      *time.Time `json:"at"`
	Label   string     `json:"Label Name"`
	Skipped string     `json:"-"`
	Upper   int32      `json:"upper"`
	Plain   bool
}

func decimal(unscaled int64, scale uint8) sglDriverDecimal {
	return sglDriverDecimal{Value: big.NewInt(unscaled), Width: 18, Scale: scale}
}

func readComposite[T any](source any) (any, error) {
	var target T
	err := sglScanDriverComposite(&target).Scan(source)
	return target, err
}

func TestScanDriverCompositeReadsTheDriversValuesIntoTheGoType(t *testing.T) {
	t.Parallel()

	moment := time.Date(2024, 3, 5, 10, 11, 12, 500, time.UTC)
	day := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	anchor := "anchor"
	label := "Label Name"

	testCases := []struct {
		source any
		want   any
		read   func(source any) (any, error)
		name   string
	}{
		{name: "NULL leaves the zero value", source: nil, read: readComposite[[]time.Time], want: []time.Time(nil)},
		{name: "a list of nullable strings", source: []any{anchor, nil}, read: readComposite[[]*string], want: []*string{&anchor, nil}},
		{name: "an empty list", source: []any{}, read: readComposite[[]int32], want: []int32{}},
		{name: "a narrower float widens", source: []any{float32(0.5)}, read: readComposite[[]float64], want: []float64{0.5}},
		{name: "an infinite float reads", source: []any{math.Inf(1)}, read: readComposite[[]float64], want: []float64{math.Inf(1)}},
		{name: "the largest unsigned integer", source: []any{uint64(math.MaxUint64)}, read: readComposite[[]uint64], want: []uint64{math.MaxUint64}},
		{name: "a string reads into a named string", source: []any{"calm"}, read: readComposite[[]scannedMood], want: []scannedMood{"calm"}},
		{name: "nested lists", source: []any{[]any{int32(1)}, []any{}}, read: readComposite[[][]int32], want: [][]int32{{1}, {}}},
		{name: "a byte slice", source: []any{[]byte("raw")}, read: readComposite[[][]byte], want: [][]byte{[]byte("raw")}},
		{name: "a big integer", source: []any{big.NewInt(7)}, read: readComposite[[]*big.Int], want: []*big.Int{big.NewInt(7)}},
		{
			name:   "a struct by its fields' json names, ignoring case when no name matches exactly",
			source: map[string]any{"at": moment, label: "pier", "UPPER": int32(3), "-": "ignored", "Plain": true, "extra": 1},
			read:   readComposite[scannedStop],
			want:   scannedStop{At: &moment, Label: "pier", Upper: 3, Plain: true},
		},
		{name: "a nullable struct", source: map[string]any{"upper": int32(1)}, read: readComposite[*scannedStop], want: &scannedStop{Upper: 1}},
		{name: "a NULL struct", source: nil, read: readComposite[*scannedStop], want: (*scannedStop)(nil)},
		{
			name:   "an ordered map keyed by dates",
			source: scannedOrderedMap{keys: []any{day}, values: []any{int32(2)}},
			read:   readComposite[map[time.Time]int32],
			want:   map[time.Time]int32{day: 2},
		},
		{
			name:   "an ordered map keyed by floats with a NULL value",
			source: scannedOrderedMap{keys: []any{1.5, 0.25}, values: []any{"x", nil}},
			read:   readComposite[map[float64]*string],
			want:   map[float64]*string{1.5: &[]string{"x"}[0], 0.25: nil},
		},
		{name: "a Go map keyed by booleans", source: map[any]any{true: int32(1)}, read: readComposite[map[bool]int32], want: map[bool]int32{true: 1}},
		{
			name:   "a map of structs holding lists",
			source: scannedOrderedMap{keys: []any{"k"}, values: []any{map[string]any{"at": moment}}},
			read:   readComposite[map[string]scannedStop],
			want:   map[string]scannedStop{"k": {At: &moment}},
		},
		{name: "a decimal through its text", source: []any{decimal(150, 2)}, read: readComposite[[]scannedExact], want: []scannedExact{{text: "1.50"}}},
		{name: "a decimal into a string", source: []any{decimal(-5, 2)}, read: readComposite[[]string], want: []string{"-0.05"}},
		{name: "a decimal into a float", source: []any{decimal(25, 1)}, read: readComposite[[]float64], want: []float64{2.5}},
		{name: "a nullable decimal", source: decimal(3, 0), read: readComposite[*scannedExact], want: &scannedExact{text: "3"}},
		{name: "an interval as the text DuckDB casts it to", source: []any{sglDriverInterval{Days: 1, Months: 0, Micros: 5_400_000_000}}, read: readComposite[[]string], want: []string{"1 day 01:30:00"}},
		{name: "a scanner reads the driver's value", source: []any{[]byte{1, 2, 3, 4}}, read: readComposite[[]scannedIdentifier], want: []scannedIdentifier{{1, 2, 3, 4}}},
		{
			name:   "a JSON document is encoded rather than scanned",
			source: []any{"text", map[string]any{"a": 1.0}, nil},
			read:   readComposite[[]scannedDocument],
			want:   []scannedDocument{scannedDocument(`"text"`), scannedDocument(`{"a":1}`), nil},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := testCase.read(testCase.source)

			require.NoError(t, err)
			require.Equal(t, testCase.want, read)
		})
	}
}

func TestScanDriverCompositeRefusesAValueItCannotRead(t *testing.T) {
	t.Parallel()

	moment := time.Date(2024, 3, 5, 10, 11, 12, 500, time.UTC)

	testCases := []struct {
		source any
		read   func(source any) (any, error)
		name   string
		want   string
	}{
		{name: "text into a list", source: "text", read: readComposite[[]int32], want: "cannot read string into []int32"},
		{name: "an integer too wide", source: []any{int64(1) << 40}, read: readComposite[[]int32], want: "element 0: 1099511627776 overflows int32"},
		{name: "a negative integer into an unsigned one", source: []any{int32(-1)}, read: readComposite[[]uint32], want: "element 0: -1 overflows uint32"},
		{name: "a number into a string", source: []any{int32(65)}, read: readComposite[[]string], want: "element 0: cannot read int32 into string"},
		{name: "a list into a map", source: []any{1}, read: readComposite[map[string]int32], want: "cannot read []interface {} into map[string]int32"},
		{name: "a list into a struct", source: []any{1}, read: readComposite[scannedStop], want: "cannot read []interface {} into source.scannedStop"},
		{name: "a field of the wrong type", source: map[string]any{"upper": "high"}, read: readComposite[scannedStop], want: "field upper: cannot read string into int32"},
		{name: "a scanner refusing the value", source: []any{[]byte{1}}, read: readComposite[[]scannedIdentifier], want: "element 0: " + errIdentifierLength.Error()},
		{name: "an interval into a number", source: []any{sglDriverInterval{Days: 1}}, read: readComposite[[]int64], want: "element 0: cannot read source.sglDriverInterval into int64"},
		{name: "a decimal into an integer", source: []any{decimal(1, 0)}, read: readComposite[[]int64], want: "element 0: cannot read a decimal into int64"},
		{name: "a NULL element into a time", source: []any{moment, nil}, read: readComposite[[]time.Time], want: "element 1: converting NULL to time.Time is unsupported"},
		{name: "a NULL element into an integer", source: []any{int32(1), nil}, read: readComposite[[]int32], want: "element 1: converting NULL to int32 is unsupported"},
		{
			name:   "a NULL map value into a string",
			source: scannedOrderedMap{keys: []any{"k"}, values: []any{nil}},
			read:   readComposite[map[string]string],
			want:   "value of key k: converting NULL to string is unsupported",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := testCase.read(testCase.source)

			require.ErrorContains(t, err, testCase.want)
			require.ErrorContains(t, err, "sgl: read ")
		})
	}
}

func TestDriverIntervalTextMatchesTheTextDuckDBCastsAnIntervalTo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		want     string
		interval sglDriverInterval
	}{
		{want: "00:00:00", interval: sglDriverInterval{Days: 0, Months: 0, Micros: 0}},
		{want: "1 day", interval: sglDriverInterval{Days: 1, Months: 0, Micros: 0}},
		{want: "2 days", interval: sglDriverInterval{Days: 2, Months: 0, Micros: 0}},
		{want: "-1 day", interval: sglDriverInterval{Days: -1, Months: 0, Micros: 0}},
		{want: "1 month", interval: sglDriverInterval{Days: 0, Months: 1, Micros: 0}},
		{want: "1 year 2 months", interval: sglDriverInterval{Days: 0, Months: 14, Micros: 0}},
		{want: "-1 year -2 months", interval: sglDriverInterval{Days: 0, Months: -14, Micros: 0}},
		{want: "1 year", interval: sglDriverInterval{Days: 0, Months: 12, Micros: 0}},
		{want: "-1 year", interval: sglDriverInterval{Days: 0, Months: -12, Micros: 0}},
		{want: "01:30:00", interval: sglDriverInterval{Days: 0, Months: 0, Micros: 5_400_000_000}},
		{want: "00:00:01.5", interval: sglDriverInterval{Days: 0, Months: 0, Micros: 1_500_000}},
		{want: "-00:00:01.5", interval: sglDriverInterval{Days: 0, Months: 0, Micros: -1_500_000}},
		{want: "25:00:00", interval: sglDriverInterval{Days: 0, Months: 0, Micros: 90_000_000_000}},
		{want: "1000:00:00", interval: sglDriverInterval{Days: 0, Months: 0, Micros: 3_600_000_000_000}},
		{want: "00:00:00.000001", interval: sglDriverInterval{Days: 0, Months: 0, Micros: 1}},
		{want: "1 year 2 months 3 days 04:05:06.789", interval: sglDriverInterval{Days: 3, Months: 14, Micros: 14_706_789_000}},
		{want: "-1 year -2 months -3 days -04:05:06.789", interval: sglDriverInterval{Days: -3, Months: -14, Micros: -14_706_789_000}},
		{want: "1 day -01:00:00", interval: sglDriverInterval{Days: 1, Months: 0, Micros: -3_600_000_000}},
		{want: "-1 year 1 day", interval: sglDriverInterval{Days: 1, Months: -12, Micros: 0}},
		{want: "00:00:00.00001", interval: sglDriverInterval{Days: 0, Months: 0, Micros: 10}},
		{want: "-00:00:00.5", interval: sglDriverInterval{Days: 0, Months: 0, Micros: -500_000}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglDriverIntervalText(testCase.interval))
		})
	}
}

func TestDriverDecimalTextKeepsEveryDigitOfTheScale(t *testing.T) {
	t.Parallel()

	huge, ok := new(big.Int).SetString("123456789012345678901234567890123456", 10)
	require.True(t, ok)
	testCases := []struct {
		want    string
		decimal sglDriverDecimal
	}{
		{want: "1.50", decimal: decimal(150, 2)},
		{want: "-0.05", decimal: decimal(-5, 2)},
		{want: "0.00", decimal: decimal(0, 2)},
		{want: "-12", decimal: decimal(-12, 0)},
		{want: "0.001", decimal: decimal(1, 3)},
		{want: "1234567890123456789012345678.90123456", decimal: sglDriverDecimal{Value: huge, Width: 38, Scale: 8}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglDriverDecimalText(testCase.decimal))
		})
	}
}

type (
	overrideLabel string
	overrideCount int64
	overrideFlag  bool
	overrideBlob  []byte
)

func readOverride[T any](source any) (any, error) {
	var destination T
	err := sglScanOverride(&destination).Scan(source)
	return destination, err
}

func TestScanOverrideReadsTheDriversValueIntoTheOverridesType(t *testing.T) {
	t.Parallel()

	label := overrideLabel("gull")
	flag := overrideFlag(true)
	testCases := []struct {
		source any
		want   any
		read   func(source any) (any, error)
		name   string
	}{
		{name: "a string into a named string", source: "gull", read: readOverride[overrideLabel], want: overrideLabel("gull")},
		{name: "bytes into a named string", source: []byte("gull"), read: readOverride[overrideLabel], want: overrideLabel("gull")},
		{name: "an integer into a named integer", source: int64(7), read: readOverride[overrideCount], want: overrideCount(7)},
		{name: "text into a named integer", source: []byte("7"), read: readOverride[overrideCount], want: overrideCount(7)},
		{name: "a boolean into a named boolean", source: true, read: readOverride[overrideFlag], want: overrideFlag(true)},
		{name: "an integer into a named boolean", source: int64(1), read: readOverride[overrideFlag], want: overrideFlag(true)},
		{name: "an integer into a nullable named boolean", source: int64(1), read: readOverride[*overrideFlag], want: &flag},
		{name: "a string into json.RawMessage", source: `{"n":1}`, read: readOverride[json.RawMessage], want: json.RawMessage(`{"n":1}`)},
		{name: "a string into a named byte slice", source: "raw", read: readOverride[overrideBlob], want: overrideBlob("raw")},
		{name: "bytes into a named byte slice", source: []byte("raw"), read: readOverride[overrideBlob], want: overrideBlob("raw")},
		{name: "a string into a nullable named string", source: "gull", read: readOverride[*overrideLabel], want: &label},
		{name: "NULL into a nullable named string", source: nil, read: readOverride[*overrideLabel], want: (*overrideLabel)(nil)},
		{name: "NULL into a named byte slice", source: nil, read: readOverride[overrideBlob], want: overrideBlob(nil)},
		{name: "text into netip.Addr", source: "10.0.0.1", read: readOverride[netip.Addr], want: netip.MustParseAddr("10.0.0.1")},
		{name: "an address into netip.Addr", source: net.IP{10, 0, 0, 1}, read: readOverride[netip.Addr], want: netip.MustParseAddr("10.0.0.1")},
		{name: "an address into a named string", source: net.IP{10, 0, 0, 1}, read: readOverride[json.Number], want: json.Number("10.0.0.1")},
		{name: "a list of strings into a list of named strings", source: []string{"a", "b"}, read: readOverride[[]overrideLabel], want: []overrideLabel{"a", "b"}},
		{name: "a scanner reads the value itself", source: []byte{1, 2, 3, 4}, read: readOverride[scannedIdentifier], want: scannedIdentifier{1, 2, 3, 4}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := testCase.read(testCase.source)

			require.NoError(t, err)
			require.Equal(t, testCase.want, read)
		})
	}
}

func TestScanOverrideRefusesWhatDatabaseSQLRefuses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		source any
		read   func(source any) (any, error)
		name   string
		want   string
	}{
		{name: "NULL into a named string", source: nil, read: readOverride[overrideLabel], want: "converting NULL to"},
		{name: "text into a named integer", source: "seven", read: readOverride[overrideCount], want: "converting driver.Value type string"},
		{name: "an integer other than 0 or 1 into a named boolean", source: int64(2), read: readOverride[overrideFlag], want: "unsupported Scan"},
		{name: "text that is not an address", source: "gull", read: readOverride[netip.Addr], want: "unsupported Scan"},
		{name: "NULL into a scanner that refuses it", source: nil, read: readOverride[scannedIdentifier], want: errIdentifierLength.Error()},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := testCase.read(testCase.source)

			require.ErrorContains(t, err, testCase.want)
		})
	}
}

type overrideShow struct {
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
}

func readOverrideJSON[T any](source any) (any, error) {
	var destination T
	err := sglScanOverrideJSON(&destination).Scan(source)
	return destination, err
}

func TestScanOverrideJSONDecodesTheDocumentAsPgxReadsIt(t *testing.T) {
	t.Parallel()

	show := overrideShow{Title: "gulls", Tags: []string{"sea", "sky"}}
	testCases := []struct {
		source any
		want   any
		read   func(source any) (any, error)
		name   string
	}{
		{name: "bytes into a plain struct", source: []byte(`{"title":"gulls","tags":["sea","sky"]}`), read: readOverrideJSON[overrideShow], want: show},
		{name: "text into a plain struct", source: `{"title":"gulls","tags":["sea","sky"]}`, read: readOverrideJSON[overrideShow], want: show},
		{name: "text into a nullable struct", source: `{"title":"gulls","tags":["sea","sky"]}`, read: readOverrideJSON[*overrideShow], want: &show},
		{name: "NULL into a nullable struct", source: nil, read: readOverrideJSON[*overrideShow], want: (*overrideShow)(nil)},
		{name: "bytes into a slice of strings", source: []byte(`["sea","sky"]`), read: readOverrideJSON[[]string], want: []string{"sea", "sky"}},
		{name: "NULL into a slice of strings", source: nil, read: readOverrideJSON[[]string], want: []string(nil)},
		{name: "a document the driver decoded", source: map[string]any{"title": "gulls", "tags": []any{"sea", "sky"}}, read: readOverrideJSON[overrideShow], want: show},
		{name: "a scanner reads the value itself", source: []byte{1, 2, 3, 4}, read: readOverrideJSON[scannedIdentifier], want: scannedIdentifier{1, 2, 3, 4}},
		{name: "a nullable scanner reads the value itself", source: []byte{1, 2, 3, 4}, read: readOverrideJSON[*scannedIdentifier], want: &scannedIdentifier{1, 2, 3, 4}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := testCase.read(testCase.source)

			require.NoError(t, err)
			require.Equal(t, testCase.want, read)
		})
	}
}

func TestScanOverrideJSONRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		source any
		read   func(source any) (any, error)
		name   string
		want   string
	}{
		{name: "NULL into a plain struct", source: nil, read: readOverrideJSON[overrideShow], want: "converting NULL to source.overrideShow is unsupported"},
		{name: "a document of another shape", source: `["sea"]`, read: readOverrideJSON[overrideShow], want: "cannot unmarshal array"},
		{name: "text that is no document", source: `gulls`, read: readOverrideJSON[[]string], want: "invalid character"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := testCase.read(testCase.source)

			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func TestDriverCompositeBindsADefinedTypeOverABasicKindAsItsKind(t *testing.T) {
	t.Parallel()

	number := json.Number("7")
	testCases := []struct {
		value any
		want  any
		name  string
	}{
		{name: "a named string", value: number, want: "7"},
		{name: "a pointer to a named string", value: &number, want: "7"},
		{name: "a named integer", value: time.Duration(7), want: int64(7)},
		{name: "a named boolean", value: asn1.Flag(true), want: true},
		{name: "a named byte slice", value: json.RawMessage(`{"n":1}`), want: []byte(`{"n":1}`)},
		{name: "a list of named strings", value: []json.Number{"1", "2"}, want: []any{"1", "2"}},
		{name: "a predeclared integer keeps its width", value: []int32{1}, want: []any{int32(1)}},
		{name: "a map keyed by a named string", value: map[json.Number]time.Duration{"1": 2}, want: sglDriverMap{"1": int64(2)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglDriverComposite(testCase.value))
		})
	}
}

func TestScanDriverCompositeReadsANilPointerOfADriverReadingDocumentsAsNULL(t *testing.T) {
	t.Parallel()

	var read []map[string]any
	require.NoError(t, sglScanDriverComposite(&read).Scan([]*map[string]any{nil}))

	require.Equal(t, []map[string]any{nil}, read, "a Nullable(JSON) element the driver hands as a nil pointer reads as NULL")
}
