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

package fuzzcase

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// runtimeRowsBound keeps native inputs small enough for exact comparisons.
	runtimeRowsBound = 8

	// runtimeShapeCount bounds the first generated query shape catalogue.
	runtimeShapeCount = 4

	// runtimeValuePrefix separates control bytes from arbitrary scalar payloads.
	runtimeValuePrefix = 8

	// runtimeShapeByte locates the query shape in version 12.
	runtimeShapeByte = 4

	// runtimeMinimumByte locates the identifier filter in version 12.
	runtimeMinimumByte = 5

	// runtimeLimitByte locates the page limit in version 12.
	runtimeLimitByte = 6

	// runtimeOffsetByte locates the page offset in version 12.
	runtimeOffsetByte = 7

	// runtimeBoundaryByte locates the boundary selection in version 12.
	runtimeBoundaryByte = 3

	// runtimeCoalesceShape selects the equivalent coalesce projection.
	runtimeCoalesceShape = 3

	// runtimeNativeFlag selects a native driver API when the engine supports one.
	runtimeNativeFlag = 8

	// runtimeTransactionFlag selects rollback and commit observations.
	runtimeTransactionFlag = 16
)

// RuntimeCase describes typed values and generated-method operations independently.
type RuntimeCase struct {
	// Engine selects native declarations and documented representation limits.
	Engine string

	// Kind selects one stable value family.
	Kind string

	// Values supplies the rows inserted through generated methods.
	Values []TypedValue

	// Data retains the original replay input.
	Data []byte

	// Shape selects a semantics-preserving generated query variant.
	Shape int

	// Minimum filters on a unique integer identifier.
	Minimum int

	// Limit bounds the paginated branch while leaving Count unchanged.
	Limit int

	// Offset skips a prefix of the filtered ordered rows.
	Offset int

	// Prepared exercises the generated prepared-statement constructor.
	Prepared bool

	// Native exercises the dedicated pgx or ClickHouse emitter.
	Native bool

	// Transactions requires rollback, commit and closed-transaction checks.
	Transactions bool
}

// RuntimeCapabilities lists native generated-code representation contracts.
//
// Takes engine (string) which selects native capabilities.
//
// Returns []Capability which includes explicit exclusions alongside admitted types.
func RuntimeCapabilities(engine string) []Capability {
	capabilities := runtimeOrderedCapabilities(engine)
	for index := range capabilities {
		capability := &capabilities[index]
		if engine == engineSQLite && capability.Feature == typeFeaturePrefix+typedDecimal {
			capability.Status = capabilityUnsupportedExact
			capability.Reason = "SQLite numeric affinity does not preserve arbitrary decimal coefficients."
		}
	}
	return capabilities
}

// RuntimeSeeds reaches every admitted family, representation boundary and NULL state.
//
// Takes engine (string) which selects native representation contracts.
//
// Returns [][]byte which supplies deterministic native generated-code coverage.
func RuntimeSeeds(engine string) [][]byte {
	var seeds [][]byte
	for index, capability := range RuntimeCapabilities(engine) {
		if capability.Status != capabilitySupported {
			continue
		}
		kind := strings.TrimPrefix(capability.Feature, typeFeaturePrefix)
		for boundary := range scenarioValueCount(kind) {
			seeds = append(seeds, []byte{RuntimeVersion, byte(index), 1, byte(boundary), 0, 0, 0, 0})
		}
		seeds = append(seeds, []byte{RuntimeVersion, byte(index), 0}, []byte{RuntimeVersion, byte(index), runtimeRowsBound, 1})
		if engine != engineClickHouse || kind != typedArray {
			seeds = append(seeds, []byte{RuntimeVersion, byte(index), 2, scenarioNullFlag})
		}
	}
	if !RuntimePrepared(engine) {
		return runtimeOperationSeeds(engine, seeds)
	}
	for _, seed := range slices.Clone(seeds) {
		prepared := append([]byte(nil), seed...)
		for len(prepared) <= runtimeShapeByte {
			prepared = append(prepared, 0)
		}
		prepared[runtimeShapeByte] = runtimeShapeCount
		seeds = append(seeds, prepared)
	}
	return runtimeOperationSeeds(engine, seeds)
}

// DecodeRuntime decodes a bounded generated-code input without consulting Seagull.
//
// Takes engine (string) which selects independent native contracts.
// Takes data ([]byte) which holds the versioned input and optional scalar payload.
//
// Returns RuntimeCase which supplies independent values and operation expectations.
// Returns bool which rejects unsupported versions, sizes and engines.
func DecodeRuntime(engine string, data []byte) (RuntimeCase, bool) {
	if len(data) == 0 || len(data) > ScenarioMaxBytes || data[0] != RuntimeVersion {
		return RuntimeCase{}, false
	}
	capabilities := RuntimeCapabilities(engine)
	if len(capabilities) == 0 {
		return RuntimeCase{}, false
	}
	choice := func(index int) byte { return scenarioByte(data, index) }
	kind, supported := runtimeSelectedFamily(capabilities, choice(1))
	if !supported {
		return RuntimeCase{}, false
	}
	result := RuntimeCase{Engine: engine, Kind: kind, Values: nil, Data: append([]byte(nil), data...),
		Shape: int(choice(runtimeShapeByte)) % runtimeShapeCount, Minimum: int(choice(runtimeMinimumByte)) % (runtimeRowsBound + 1),
		Limit: int(choice(runtimeLimitByte)) % (runtimeRowsBound + 1), Offset: int(choice(runtimeOffsetByte)) % (runtimeRowsBound + 1),
		Prepared:     RuntimePrepared(engine) && choice(runtimeShapeByte)/runtimeShapeCount%2 != 0,
		Native:       RuntimeNative(engine) && choice(runtimeShapeByte)&runtimeNativeFlag != 0,
		Transactions: RuntimeTransactions(engine) && choice(runtimeShapeByte)&runtimeTransactionFlag != 0}
	if result.Native {
		result.Prepared = false
	}
	result.Values = runtimeValues(engine, kind, data, choice(2), choice(runtimeBoundaryByte))
	return result, true
}

// RuntimeSchema builds all admitted native tables for a compiled value catalogue.
//
// Takes engine (string) which selects native syntax and nullability rules.
//
// Returns []string which creates the independent model's native tables.
func RuntimeSchema(engine string) []string {
	var schema []string
	for _, capability := range RuntimeCapabilities(engine) {
		if capability.Status != capabilitySupported {
			continue
		}
		kind := strings.TrimPrefix(capability.Feature, typeFeaturePrefix)
		identifier, value, suffix := "BIGINT NOT NULL", capability.NativeType, ""
		if engine == engineClickHouse {
			identifier, suffix = "Int64", " ENGINE = Memory"
			if kind != typedArray {
				value = clickHouseNullable(value)
			}
		}
		schema = append(schema, "CREATE TABLE native_"+kind+" (id "+identifier+", value "+value+")"+suffix+";")
	}
	return schema
}

// RuntimeQueries builds a fixed generated-method catalogue for the native worker.
//
// Takes engine (string) which selects native placeholders and type declarations.
// Takes shape (int) which selects equivalent projection syntax.
//
// Returns string which includes inserts, ordered reads, streams and runtime builders.
func RuntimeQueries(engine string, shape int) string {
	var queries strings.Builder
	for _, capability := range RuntimeCapabilities(engine) {
		if capability.Status != capabilitySupported {
			continue
		}
		kind := strings.TrimPrefix(capability.Feature, typeFeaturePrefix)
		suffix := RuntimeSuffix(kind)
		first, second := "$1", "$2"
		switch engine {
		case engineMySQL, engineMariaDB:
			first, second = "?", "?"
		case engineSQLite:
			first, second = "?1", "?2"
		case engineClickHouse:
			native := capability.NativeType
			if kind != typedArray {
				native = clickHouseNullable(native)
			}
			first, second = "{id:Int64}", "{value:"+native+"}"
		}
		fmt.Fprintf(&queries, "-- sgl.query(Insert%s, exec)\nINSERT INTO native_%s (id, value) VALUES (%s, %s);\n\n", suffix, kind, first, second)
		value := "value"
		switch shape % runtimeShapeCount {
		case 1:
			value = "(value)"
		case 2:
			value = "CASE WHEN id >= 0 THEN value ELSE value END"
		case runtimeCoalesceShape:
			value = "COALESCE(value, value)"
		}
		for _, method := range []struct{ name, command string }{{"Read", "many"}, {"Stream", "stream"}, {"Browse", "many, dynamic: runtime"}} {
			fmt.Fprintf(&queries, "-- sgl.query(%s%s, %s)\nSELECT id, %s AS value, value IS NULL AS is_null FROM native_%s ORDER BY id;\n\n",
				method.name, suffix, method.command, value, kind)
		}
	}
	return queries.String()
}

// RuntimeSuffix names methods without depending on generated naming decisions.
//
// Takes kind (string) which is a validated independent value family.
//
// Returns string which is the explicit method-name suffix.
func RuntimeSuffix(kind string) string {
	if kind == "" {
		return ""
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

// runtimePayload adds arbitrary scalar mutations within native representation limits.
//
// Takes value (TypedValue) which supplies the selected family and default boundary.
// Takes payload ([]byte) which supplies additional bounded scalar data.
//
// Returns TypedValue which remains valid for its declared family.
func runtimePayload(value TypedValue, payload []byte) TypedValue {
	payload = payload[:min(len(payload), typedScalarBound)]
	switch value.Kind {
	case typedText:
		text := strings.ToValidUTF8(string(payload), "\uFFFD")
		text = strings.ReplaceAll(text, "\x00", "")
		for len(text) > typedScalarBound {
			_, size := utf8.DecodeLastRuneInString(text)
			text = text[:len(text)-size]
		}
		value.Text = text
	case typedBinary:
		value.Bytes = append([]byte{}, payload...)
	case typedSigned, typedUnsigned, typedFloat:
		var encoded [8]byte
		copy(encoded[:], payload)
		bits := binary.LittleEndian.Uint64(encoded[:])
		switch value.Kind {
		case typedSigned:
			value.Number = strconv.FormatInt(runtimeSigned(bits), decimalBase)
		case typedUnsigned:
			value.Number = strconv.FormatUint(bits, decimalBase)
		case typedFloat:
			number := math.Float64frombits(bits)
			if !math.IsNaN(number) && !math.IsInf(number, 0) {
				value.Bits = bits
			}
		}
	}
	return value
}

// runtimeSigned interprets every payload bit without an overflowing conversion.
//
// Takes bits (uint64) which holds the independent two's-complement payload.
//
// Returns int64 which preserves the full signed bit pattern.
func runtimeSigned(bits uint64) int64 {
	if bits <= math.MaxInt64 {
		return int64(bits)
	}
	magnitude := (^bits) & math.MaxInt64
	return -1 - int64(magnitude)
}

// runtimeOrderedCapabilities freezes version 12 family indices independently of
// manifests.
//
// Takes engine (string) which selects native representation contracts.
//
// Returns []Capability which retains the original thirteen family positions.
func runtimeOrderedCapabilities(engine string) []Capability {
	available := typeCapabilities(engine)
	if len(available) == 0 {
		return nil
	}
	var ordered []Capability
	for _, kind := range typedKinds {
		entry := Capability{Feature: typeFeaturePrefix + kind, NativeType: "", CastType: "", Status: capabilityUnclassified, Reason: "The independent manifest has no contract for this family."}
		for _, capability := range available {
			if capability.Feature == entry.Feature {
				entry = capability
				break
			}
		}
		ordered = append(ordered, entry)
	}
	return ordered
}

// RuntimeShapeSeeds reaches each projection shape with populated nullable rows.
//
// Takes engine (string) which selects admitted independent value families.
//
// Returns [][]byte which exercises shape compilation before mutation begins.
func RuntimeShapeSeeds(engine string) [][]byte {
	seeds := RuntimeSeeds(engine)
	modes := []byte{0}
	if RuntimePrepared(engine) {
		modes = append(modes, runtimeShapeCount)
	}
	if RuntimeNative(engine) {
		modes = append(modes, runtimeNativeFlag)
	}
	for _, mode := range modes {
		for shape := byte(1); shape < runtimeShapeCount; shape++ {
			seeds = append(seeds, runtimeProjectionSeeds(engine, shape|mode)...)
		}
	}
	return seeds
}

// runtimeProjectionSeeds covers populated and nullable rows for one projection mode.
//
// Takes engine (string) which supplies native representation contracts.
// Takes mode (byte) which combines projection and driver selection.
//
// Returns [][]byte which covers every admitted value family.
func runtimeProjectionSeeds(engine string, mode byte) [][]byte {
	var seeds [][]byte
	for index, capability := range RuntimeCapabilities(engine) {
		if capability.Status != capabilitySupported {
			continue
		}
		seeds = append(seeds, []byte{RuntimeVersion, byte(index), runtimeRowsBound, 1, mode, 2, 2, 1},
			[]byte{RuntimeVersion, byte(index), runtimeRowsBound, scenarioNullFlag, mode, 2, 2, 1})
	}
	return seeds
}

// runtimeSelectedFamily bounds selection even when every capability is excluded.
//
// Takes capabilities ([]Capability) which retains fixed family positions.
// Takes choice (byte) which selects a starting position.
//
// Returns string which names an admitted independent family.
// Returns bool which rejects empty or wholly excluded manifests.
func runtimeSelectedFamily(capabilities []Capability, choice byte) (string, bool) {
	for offset := range len(capabilities) {
		selected := (int(choice) + offset) % len(capabilities)
		if capabilities[selected].Status == capabilitySupported {
			return strings.TrimPrefix(capabilities[selected].Feature, typeFeaturePrefix), true
		}
	}
	return "", false
}

// runtimeValues builds bounded rows from independent boundaries and scalar payloads.
//
// Takes engine (string) which supplies native nullability contracts.
// Takes kind (string) which selects the independent value family.
// Takes data ([]byte) which holds the original replay envelope.
// Takes countByte (byte) which selects the bounded row count.
// Takes boundary (byte) which selects the first exact boundary value.
//
// Returns []TypedValue which contains only independently modelled values.
func runtimeValues(engine, kind string, data []byte, countByte, boundary byte) []TypedValue {
	count := int(countByte) % (runtimeRowsBound + 1)
	values := make([]TypedValue, 0, count)
	for index := range count {
		selectedValue := boundary + byte(index)
		if engine == engineClickHouse && kind == typedArray {
			selectedValue &= ^byte(scenarioNullFlag)
		}
		value := scenarioValue(kind, selectedValue)
		if index == 0 && len(data) > runtimeValuePrefix && !value.Null {
			value = runtimePayload(value, data[runtimeValuePrefix:])
		}
		values = append(values, value)
	}
	return values
}
