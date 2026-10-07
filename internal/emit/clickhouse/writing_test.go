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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestUndecodableMapKeyFindsAKeyTheDriverCannotDecodeAtAnyDepth(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}
	wide := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, Width: 256}
	address := sqlmodel.NewNetworkType("IPv6", sqlmodel.NetworkKindAddress)
	text := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}
	decimal := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryDecimal}
	uuid := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUUID}
	date := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryTemporal, Temporal: sqlmodel.TemporalKindDate}
	enum := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryEnum, EnumValues: []string{"a", "b"}}
	keyed := func(key sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &key, ElementType: &text}
	}
	textMapOf := func(value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &text, ElementType: &value}
	}

	testCases := []struct {
		name    string
		key     string
		sqlType sqlmodel.SQLType
	}{
		{name: "a map keyed by an address", sqlType: keyed(address), key: "an IPv4 or IPv6 address"},
		{name: "a map keyed by an array", sqlType: keyed(arrayOf(integer)), key: "an Array"},
		{name: "a map keyed by a named tuple", sqlType: keyed(namedOf(integer)), key: "a Tuple"},
		{name: "a map keyed by an unnamed tuple", sqlType: keyed(unnamedOf(integer, text)), key: "a Tuple"},
		{name: "a map keyed by a map", sqlType: keyed(textMapOf(text)), key: "a Map"},
		{name: "a map keyed by a decimal", sqlType: keyed(decimal), key: "a Decimal"},
		{name: "an array of maps keyed by an address", sqlType: arrayOf(keyed(address)), key: "an IPv4 or IPv6 address"},
		{name: "a named tuple holding a map keyed by an array", sqlType: namedOf(integer, keyed(arrayOf(text))), key: "an Array"},
		{name: "a map to maps keyed by a decimal", sqlType: textMapOf(keyed(decimal)), key: "a Decimal"},
		{name: "a map keyed by text", sqlType: keyed(text), key: ""},
		{name: "a map keyed by a wide integer", sqlType: keyed(wide), key: ""},
		{name: "a map keyed by a UUID", sqlType: keyed(uuid), key: ""},
		{name: "a map keyed by a date", sqlType: keyed(date), key: ""},
		{name: "a map keyed by an enum", sqlType: keyed(enum), key: ""},
		{name: "a map to addresses", sqlType: textMapOf(address), key: ""},
		{name: "an array of addresses", sqlType: arrayOf(address), key: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			key, undecodable := undecodableMapKey(&testCase.sqlType, 0)
			require.Equal(t, testCase.key, key)
			require.Equal(t, testCase.key != "", undecodable)
		})
	}
}

func arrayOf(element sqlmodel.SQLType) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &element}
}

func namedOf(parts ...sqlmodel.SQLType) sqlmodel.SQLType {
	fields := make([]sqlmodel.StructField, 0, len(parts))
	for index := range parts {
		fields = append(fields, sqlmodel.StructField{Name: string(rune('a' + index)), SQLType: parts[index]})
	}
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: fields}
}

func unnamedOf(parts ...sqlmodel.SQLType) sqlmodel.SQLType {
	fields := make([]sqlmodel.StructField, 0, len(parts))
	for index := range parts {
		fields = append(fields, sqlmodel.StructField{Name: string(rune('1' + index)), Positional: true, SQLType: parts[index]})
	}
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: fields}
}
