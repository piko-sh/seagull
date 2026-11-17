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

package fuzznative

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// decimalScaleBound matches the independent exact-decimal wire limit.
const decimalScaleBound = 38

// canonicalNumber distinguishes exact JSON numbers from JSON strings.
type canonicalNumber string

// Problems checks every required native generated-code observation independently.
//
// Takes model (fuzzcase.RuntimeCase) which supplies expected values and operations.
// Takes response (*Response) which contains actual generated-code observations.
//
// Returns []string which lists missing stages, errors and semantic differences.
func Problems(model fuzzcase.RuntimeCase, response *Response) []string {
	var problems []string
	if model.Native != response.Native {
		problems = append(problems, "generated driver API differs from the required model")
	}
	if model.Prepared != response.Prepared {
		problems = append(problems, "generated preparation mode differs from the required model")
	}
	if response.Error != "" || response.Panic != "" {
		return append(problems, fmt.Sprintf("generated %s failed: %s %s", response.Stage, response.Error, response.Panic))
	}
	if !slices.Equal(runStageNames(), response.Completed) {
		problems = append(problems, "generated execution did not complete every required stage")
	}
	expected := make([]Row, len(model.Values))
	var filtered []Row
	for index, value := range model.Values {
		row := Row{ID: int64(index + 1), Value: value, Null: value.Null}
		expected[index] = row
		if row.ID >= int64(model.Minimum) {
			filtered = append(filtered, row)
		}
	}
	page := filtered[min(model.Offset, len(filtered)):]
	if model.Limit > 0 {
		page = page[:min(model.Limit, len(page))]
	}
	for _, observation := range []struct {
		name      string
		want, got []Row
	}{
		{name: "plain", want: expected, got: response.Plain},
		{name: "stream", want: expected, got: response.Stream},
		{name: "early", want: expected[:min(1, len(expected))], got: response.Early},
		{name: "page", want: page, got: response.Page},
		{name: "reuse", want: page, got: response.Reused},
		{name: "fresh", want: expected, got: response.Fresh},
	} {
		if err := CompareRows(observation.want, observation.got); err != nil {
			problems = append(problems, observation.name+": "+err.Error())
		}
	}
	if response.Count != int64(len(filtered)) {
		problems = append(problems, "Count disagrees with the filtered unpaginated model")
	}
	if response.EmptyCount != 0 {
		problems = append(problems, "an empty IN list returned rows")
	}
	return append(problems, lifecycleProblems(model, response, expected)...)
}

// CompareRows checks identity, total ordering, NULL masks and exact typed values.
//
// Takes expected ([]Row) which comes from an independent model or native reference.
// Takes observed ([]Row) which comes from generated Go.
//
// Returns error when row count, order or any semantic value differs.
func CompareRows(expected, observed []Row) error {
	if len(expected) != len(observed) {
		return fmt.Errorf("row count differs: expected %d, observed %d", len(expected), len(observed))
	}
	for index := range expected {
		want, got := &expected[index], &observed[index]
		if want.ID != got.ID || want.Null != got.Null {
			return fmt.Errorf("row %d has a different identity or NULL mask: expected (%d, %t), observed (%d, %t)", index, want.ID, want.Null, got.ID, got.Null)
		}
		if err := CompareValue(want.Value, got.Value); err != nil {
			return fmt.Errorf("row %d: %w", index, err)
		}
	}
	return nil
}

// CompareValue compares values without converting exact numbers through float64.
//
// Takes expected (fuzzcase.TypedValue) which is the independent expected value.
// Takes observed (fuzzcase.TypedValue) which is the native generated observation.
//
// Returns error when the values differ under their explicit family contract.
func CompareValue(expected, observed fuzzcase.TypedValue) error {
	if expected.Kind != observed.Kind || expected.Null != observed.Null {
		return errors.New("value family or SQL NULL state differs")
	}
	if expected.Null {
		return nil
	}
	equal, err := equalValue(expected, observed)
	if err != nil {
		return err
	}
	if !equal {
		return fmt.Errorf("%s value changed during generated execution", expected.Kind)
	}
	return nil
}

// equalValue applies the explicit comparison rule of one independent family.
//
// Takes expected (fuzzcase.TypedValue) which supplies the reference value.
// Takes observed (fuzzcase.TypedValue) which supplies the actual value.
//
// Returns bool which reports semantic equality within that family.
// Returns error when either representation cannot be read without precision loss.
func equalValue(expected, observed fuzzcase.TypedValue) (bool, error) {
	switch expected.Kind {
	case kindSigned, kindUnsigned, kindBoolean:
		return expected.Number == observed.Number, nil
	case kindFloat:
		if expected.Bits == observed.Bits {
			return true, nil
		}
		return math.Float64frombits(expected.Bits) == 0 && math.Float64frombits(observed.Bits) == 0, nil
	case kindDecimal:
		return equalDecimal(expected, observed)
	case kindText:
		return expected.Text == observed.Text, nil
	case kindUUID:
		return strings.EqualFold(expected.Text, observed.Text), nil
	case kindBinary:
		return bytes.Equal(expected.Bytes, observed.Bytes), nil
	case kindDate, kindTimestamp, kindTimestampTZ:
		left, err := valueTime(expected)
		if err != nil {
			return false, err
		}
		right, err := valueTime(observed)
		return err == nil && left.Equal(right), err
	case kindJSON:
		left, err := exactJSON(expected.Text)
		if err != nil {
			return false, err
		}
		right, err := exactJSON(observed.Text)
		return err == nil && reflect.DeepEqual(left, right), err
	case kindArray:
		return equalArray(expected, observed)
	default:
		return false, errors.New("unknown native comparison family")
	}
}

// exactJSON retains arbitrary integer digits and compares JSON numbers mathematically.
//
// Takes text (string) which contains one valid JSON document.
//
// Returns any which distinguishes exact numbers, strings, objects and arrays.
// Returns error when the document is invalid.
func exactJSON(text string) (any, error) {
	if !json.Valid([]byte(text)) {
		return nil, errors.New("invalid JSON observation")
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return normaliseJSON(value)
}

// normaliseJSON canonicalises numeric spellings while preserving JSON value kinds.
//
// Takes value (any) which was decoded with json.Decoder.UseNumber.
//
// Returns any which can be compared without converting numbers through float64.
// Returns error when a numeric spelling cannot be represented exactly.
func normaliseJSON(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		return canonicalJSONNumber(string(typed))
	case []any:
		for index, element := range typed {
			normalised, err := normaliseJSON(element)
			if err != nil {
				return nil, err
			}
			typed[index] = normalised
		}
	case map[string]any:
		for key, element := range typed {
			normalised, err := normaliseJSON(element)
			if err != nil {
				return nil, err
			}
			typed[key] = normalised
		}
	}
	return value, nil
}

// canonicalJSONNumber compares exact numbers without expanding their exponents.
//
// Takes text (string) which contains a number accepted by the JSON decoder.
//
// Returns canonicalNumber which uses memory proportional to the input spelling.
// Returns error when the exponent is not a base-ten integer.
func canonicalJSONNumber(text string) (canonicalNumber, error) {
	mantissa, exponent, _ := strings.Cut(strings.ToLower(text), "e")
	negative := strings.HasPrefix(mantissa, numberNegative)
	mantissa = strings.TrimPrefix(mantissa, numberNegative)
	integer, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		return canonicalNumber(numberZero), nil
	}
	coefficient := strings.TrimRight(digits, "0")
	scale := new(big.Int)
	if exponent != "" {
		if _, ok := scale.SetString(exponent, scalarBase); !ok {
			return "", fmt.Errorf("JSON number %q has an invalid exponent", text)
		}
	}
	scale.Add(scale, big.NewInt(int64(len(digits)-len(coefficient)-len(fraction))))
	if negative {
		coefficient = numberNegative + coefficient
	}
	return canonicalNumber(coefficient + "e" + scale.String()), nil
}

// observedDecimal converts native decimal text into a coefficient and bounded scale.
//
// Takes text (string) which comes from the actual generated decimal value.
//
// Returns fuzzcase.TypedValue which retains every decimal digit.
// Returns error when the text is invalid or requires excessive decimal precision.
func observedDecimal(text string) (fuzzcase.TypedValue, error) {
	number, ok := new(big.Rat).SetString(text)
	if !ok {
		return fuzzcase.TypedValue{}, errors.New("invalid generated decimal text")
	}
	factor := new(big.Int).Exp(big.NewInt(scalarBase), big.NewInt(decimalScaleBound), nil)
	number.Mul(number, new(big.Rat).SetInt(factor))
	if !number.IsInt() {
		return fuzzcase.TypedValue{}, errors.New("generated decimal exceeds the exact scale bound")
	}
	coefficient := number.Num().String()
	scale := decimalScaleBound
	for scale > 0 && strings.HasSuffix(coefficient, numberZero) {
		coefficient = strings.TrimSuffix(coefficient, numberZero)
		scale--
	}
	if coefficient == "" || coefficient == numberNegative {
		coefficient = numberZero
	}
	result := fuzzcase.TypedNumber(kindDecimal, coefficient, scale)
	return result, result.Validate()
}

// observedUnsigned retains full unsigned range even when the generated type is decimal.
//
// Takes text (string) which comes from the actual generated numeric value.
//
// Returns string which contains the exact base-ten unsigned integer.
// Returns error when the native value is fractional or outside uint64.
func observedUnsigned(text string) (string, error) {
	number, ok := new(big.Rat).SetString(text)
	if !ok || !number.IsInt() || !number.Num().IsUint64() {
		return "", errors.New("generated unsigned value is not an exact uint64")
	}
	return strconv.FormatUint(number.Num().Uint64(), scalarBase), nil
}

// CanonicalTime records a native temporal scalar using its independent family.
//
// Takes kind (string) which distinguishes dates, wall times and instants.
// Takes value (time.Time) which is the native driver observation.
//
// Returns fuzzcase.TypedValue which retains the appropriate temporal meaning.
func CanonicalTime(kind string, value time.Time) fuzzcase.TypedValue {
	result := fuzzcase.TypedNumber(kind, numberZero, 0)

	_ = observeTime(&result, reflect.ValueOf(value))
	return result
}

// equalDecimal compares independent decimal values without precision loss.
//
// Takes expected (fuzzcase.TypedValue) which supplies the model value.
// Takes observed (fuzzcase.TypedValue) which supplies the actual generated value.
//
// Returns bool which reports semantic equality.
// Returns error when either value is invalid or an element differs.
func equalDecimal(expected, observed fuzzcase.TypedValue) (bool, error) {
	left, ok := new(big.Rat).SetString(expected.Decimal())
	if !ok {
		return false, errors.New("invalid expected decimal")
	}
	right, ok := new(big.Rat).SetString(observed.Decimal())
	if !ok {
		return false, errors.New("invalid observed decimal")
	}
	return left.Cmp(right) == 0, nil
}

// equalArray compares independent array values without precision loss.
//
// Takes expected (fuzzcase.TypedValue) which supplies the model value.
// Takes observed (fuzzcase.TypedValue) which supplies the actual generated value.
//
// Returns bool which reports semantic equality.
// Returns error when either value is invalid or an element differs.
func equalArray(expected, observed fuzzcase.TypedValue) (bool, error) {
	if len(expected.Elements) != len(observed.Elements) {
		return false, nil
	}
	for index, element := range expected.Elements {
		if err := CompareValue(element, observed.Elements[index]); err != nil {
			return false, err
		}
	}
	return true, nil
}

// lifecycleProblems checks transaction persistence and cancellation recovery
// independently.
//
// Takes model (fuzzcase.RuntimeCase) which supplies independent transaction expectations.
// Takes response (*Response) which records native observations.
// Takes baseline ([]Row) which contains the original modelled rows.
//
// Returns []string which reports missing or corrupted lifecycle observations.
func lifecycleProblems(model fuzzcase.RuntimeCase, response *Response, baseline []Row) []string {
	var problems []string
	final := baseline
	if model.Transactions {
		value := fuzzcase.RuntimeTransactionValue(model)
		final = append(append([]Row(nil), baseline...), Row{ID: transactionIdentifier, Value: value, Null: value.Null})
		for _, observation := range []struct {
			name      string
			want, got []Row
		}{
			{name: "inside rollback", want: final, got: response.BeforeRollback},
			{name: "after rollback", want: baseline, got: response.AfterRollback},
			{name: "inside commit", want: final, got: response.BeforeCommit},
			{name: "after commit", want: final, got: response.AfterCommit},
		} {
			if err := CompareRows(observation.want, observation.got); err != nil {
				problems = append(problems, observation.name+": "+err.Error())
			}
		}
		if response.ClosedTransactions != closedTransactionChecks {
			problems = append(problems, "completed transactions were not both rejected")
		}
	} else if len(response.BeforeRollback)+len(response.AfterRollback)+len(response.BeforeCommit)+len(response.AfterCommit)+response.ClosedTransactions != 0 {
		problems = append(problems, "unsupported or unrequested transaction operations executed")
	}
	if !response.Cancelled {
		problems = append(problems, "cancelled context was not preserved")
	}
	if err := CompareRows(final, response.AfterCancel); err != nil {
		problems = append(problems, "cancellation recovery: "+err.Error())
	}
	return problems
}
