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

const (
	// capabilitySupported marks an admitted feature or exact contract.
	capabilitySupported = "supported"

	// capabilityUnsupportedNative marks a feature the engine or fixture cannot express.
	capabilityUnsupportedNative = "unsupported-native"

	// capabilityUnsupportedExact marks a feature the engine cannot represent exactly.
	capabilityUnsupportedExact = "unsupported-exact"

	// capabilityUnclassified marks a family the manifest has no contract for.
	capabilityUnclassified = "unclassified"

	// typeFeaturePrefix names independent type obligations.
	typeFeaturePrefix = "type/"
)

// typedKinds lists the value families in their permanent order.
var typedKinds = []string{typedSigned, typedUnsigned, typedDecimal, typedFloat, typedBoolean, typedText, typedBinary,
	typedDate, typedTimestamp, typedTimestamptz, typedUUID, typedJSON, typedArray}

// defaultNativeTypes names the column type of each family on engines without an override.
var defaultNativeTypes = map[string]string{typedSigned: "BIGINT", typedUnsigned: "NUMERIC(20,0)", typedDecimal: "DECIMAL(38,6)",
	typedFloat: "DOUBLE PRECISION", typedBoolean: "BOOLEAN", typedText: "TEXT", typedBinary: "BYTEA", typedDate: "DATE",
	typedTimestamp: "TIMESTAMP", typedTimestamptz: "TIMESTAMPTZ", typedUUID: "UUID", typedJSON: "JSONB", typedArray: "BIGINT[]"}

// clickHouseNativeTypes names the ClickHouse column type of each family without a
// nullable wrapper.
var clickHouseNativeTypes = map[string]string{typedSigned: "Int64", typedUnsigned: "UInt64", typedDecimal: "Decimal(38,6)",
	typedFloat: "Float64", typedBoolean: "Bool", typedText: "String", typedBinary: "String", typedDate: "Date32",
	typedTimestamp: "DateTime64(6)", typedTimestamptz: "DateTime64(6, 'UTC')", typedUUID: "UUID", typedArray: "Array(Int64)"}

// Capability describes an explicitly admitted type or operation.
type Capability struct {
	// Feature identifies an independent coverage obligation.
	Feature string

	// NativeType names the type in a column declaration.
	NativeType string

	// CastType names the type in an explicit conversion.
	CastType string

	// Status distinguishes admitted features from declared limitations.
	Status string

	// Reason explains limitations without relying on a Seagull rejection.
	Reason string
}

// typeCapabilities lists the independent type contracts for an engine.
//
// Takes engine (string) which selects native type names.
//
// Returns []Capability which includes both admitted and excluded families.
func typeCapabilities(engine string) []Capability {
	if !supportsShape(engine, shapeSelect) {
		return nil
	}
	var result []Capability
	for _, kind := range typedKinds {
		native, cast := scenarioType(engine, kind)
		capability := Capability{Feature: typeFeaturePrefix + kind, NativeType: native, CastType: cast, Status: capabilitySupported, Reason: ""}
		if native == "" {
			capability.Status = capabilityUnsupportedNative
			capability.Reason = "The fixture does not provide this native type and binding contract."
		}
		result = append(result, capability)
	}
	return result
}

// scenarioType chooses native declarations without consulting the compiler.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes kind (string) which selects the independent value family.
//
// Returns declaration (string) which names the native column type.
// Returns conversion (string) which names the native cast type.
func scenarioType(engine, kind string) (declaration, conversion string) {
	if engine == engineClickHouse {
		return clickHouseScenarioType(kind)
	}
	native := defaultNativeTypes[kind]
	switch engine {
	case engineMySQL, engineMariaDB:
		return mysqlScenarioType(engine, kind)
	case engineSQLite:
		switch kind {
		case typedUnsigned, typedUUID, typedArray, typedTimestamptz:
			return "", ""
		case typedBinary:
			native = "BLOB"
		case typedDate, typedTimestamp, typedJSON:
			native = "TEXT"
		}
	case engineDuckDB:
		switch kind {
		case typedUnsigned:
			native = "UBIGINT"
		case typedBinary:
			native = "BLOB"
		case typedJSON:
			native = typedJSONType
		}
	}
	return native, native
}

// mysqlScenarioType retains differences between column and cast syntax.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes kind (string) which selects the independent value family.
//
// Returns declaration (string) which names the native column type.
// Returns conversion (string) which names the native cast type.
func mysqlScenarioType(engine, kind string) (declaration, conversion string) {
	switch kind {
	case typedSigned:
		return "BIGINT", "SIGNED"
	case typedUnsigned:
		return "BIGINT UNSIGNED", "UNSIGNED"
	case typedDecimal:
		return "DECIMAL(38,6)", "DECIMAL(38,6)"
	case typedFloat:
		return "DOUBLE", "DOUBLE"
	case typedBoolean:
		return "BOOLEAN", "UNSIGNED"
	case typedText:
		return "TEXT", "CHAR"
	case typedBinary:
		return "BLOB", "BINARY"
	case typedDate:
		return "DATE", "DATE"
	case typedTimestamp:
		return "DATETIME(6)", "DATETIME(6)"
	case typedJSON:
		if engine == engineMariaDB {
			return typedJSONType, "CHAR"
		}
		return typedJSONType, typedJSONType
	default:
		return "", ""
	}
}

// clickHouseScenarioType declares scalar types without nullable wrappers.
//
// Takes kind (string) which selects the independent value family.
//
// Returns declaration (string) which names the native column type.
// Returns conversion (string) which names the native cast type.
func clickHouseScenarioType(kind string) (declaration, conversion string) {
	native := clickHouseNativeTypes[kind]
	return native, native
}
