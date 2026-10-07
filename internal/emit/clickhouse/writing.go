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

package clickhouse

import (
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

// appendsThroughConversion reports whether a query of the request appends a value to a
// native batch through the conversion, as appendsRowThroughConversion says, so the
// package needs its file.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the request's queries.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when a query appends such a value.
func appendsThroughConversion(queries []*sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for _, query := range queries {
		if appendsColumns(query) && appendsRowThroughConversion(query, types) {
			return true
		}
	}
	return false
}

// appendsRowThroughConversion reports whether a native batch row needs value conversion.
//
// Overrides and values checked by chvalues.Appends use sglAppendNativeRow.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes types (sqlmodel.GoTypeResolver) which maps the request's SQL types.
//
// Returns bool which is true when a parameter's value is appended through it.
func appendsRowThroughConversion(query *sqlmodel.AnalysedQuery, types sqlmodel.GoTypeResolver) bool {
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		if chvalues.Appends(&parameter.SQLType) || gogen.OverrideChosen(types, parameter.GoTypeOverride, parameter.SQLType) {
			return true
		}
	}
	return false
}

// appendProblems identifies native batch columns with unsupported map keys.
//
// The driver cannot prepare these columns, so each produces an emitter-unsupported
// diagnostic.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query, whose values are appended as
// columns.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func appendProblems(query *sqlmodel.AnalysedQuery) []emit.Problem {
	var problems []emit.Problem
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		key, undecodable := undecodableMapKey(&parameter.SQLType, 0)
		if !undecodable {
			continue
		}
		problems = append(problems, emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: fmt.Sprintf("parameter %s holds a Map keyed by %s, which the native ClickHouse driver cannot decode, so it "+
				"cannot prepare a batch for the column; write the rows with command: exec, which binds the Map by name as text, "+
				"or key the Map by text", parameter.Name, key),
			Severity: diag.SeverityError,
		})
	}
	return problems
}

// undecodableMapKey finds nested Map keys the native driver cannot decode.
//
// Address, collection and Decimal keys are unsupported (verified on clickhouse-go v2.48).
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
// Takes depth (int) which is how deep the type is nested.
//
// Returns string which describes the key, such as "an Array".
// Returns bool which is true when such a Map is found.
func undecodableMapKey(sqlType *sqlmodel.SQLType, depth int) (string, bool) {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return "", false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryMap:
		if key, undecodable := undecodableKey(sqlType.KeyType); undecodable {
			return key, true
		}
		return undecodableMapKey(sqlType.ElementType, depth+1)
	case sqlmodel.TypeCategoryArray:
		return undecodableMapKey(sqlType.ElementType, depth+1)
	case sqlmodel.TypeCategoryStruct:
		for index := range sqlType.StructFields {
			if key, undecodable := undecodableMapKey(&sqlType.StructFields[index].SQLType, depth+1); undecodable {
				return key, true
			}
		}
		return "", false
	default:
		return "", false
	}
}

// undecodableKey describes a Map's key type the native driver cannot decode.
//
// Takes key (*sqlmodel.SQLType) which is the key type, or nil.
//
// Returns string which describes the key.
// Returns bool which is true when the driver cannot decode it.
func undecodableKey(key *sqlmodel.SQLType) (string, bool) {
	if key == nil {
		return "", false
	}
	switch key.Category {
	case sqlmodel.TypeCategoryNetwork:
		return "an IPv4 or IPv6 address", key.Network == sqlmodel.NetworkKindAddress
	case sqlmodel.TypeCategoryArray:
		return "an Array", true
	case sqlmodel.TypeCategoryStruct:
		return "a Tuple", true
	case sqlmodel.TypeCategoryMap:
		return "a Map", true
	case sqlmodel.TypeCategoryDecimal:
		return "a Decimal", true
	default:
		return "", false
	}
}
