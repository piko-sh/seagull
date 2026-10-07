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

package pgx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestReaderCastsToTextWhatPgxCannotScanIntoAString(t *testing.T) {
	t.Parallel()

	bits := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "BITS")
	bits.BinaryForm = true
	interval := sqlmodel.NewTemporalType("SPAN", sqlmodel.TemporalKindInterval, false)
	duration := sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Package: "time", Name: "Duration"}}
	text := sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Name: "string"}}
	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		slots   sqlmodel.TypeSlots
		want    gogen.ColumnReading
	}{
		{name: "text sent in another binary form", sqlType: bits, want: gogen.ColumnReadAsText},
		{name: "text sent as text", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "TEXT"), want: gogen.ColumnReadAsIs},
		{name: "an interval read into a time.Duration", sqlType: interval, slots: sqlmodel.TypeSlots{Interval: duration}, want: gogen.ColumnReadAsIs},
		{name: "an interval read into a string", sqlType: interval, slots: sqlmodel.TypeSlots{Interval: text}, want: gogen.ColumnReadAsText},
		{name: "a zoned time of day, sent as text", sqlType: sqlmodel.NewTemporalType("CLOCKZ", sqlmodel.TemporalKindTime, true), want: gogen.ColumnReadAsIs},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			reader := pgxReader{types: gotypes.NewTypeMapper(nil).BuildMappingTable(nil, testCase.slots)}
			require.Equal(t, testCase.want, reader.ColumnReading(testCase.sqlType))
		})
	}
}

func TestReaderWithoutTypesReadsEveryColumnAsItIs(t *testing.T) {
	t.Parallel()

	bits := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "BITS")
	bits.BinaryForm = true
	require.Equal(t, gogen.ColumnReadAsIs, pgxReader{types: nil}.ColumnReading(bits))
}
