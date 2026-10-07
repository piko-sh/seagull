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

package mysql

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

type expectedColumn struct {
	name       string
	engineName string
	nullable   bool
	hasDefault bool
}

func columnShapes(columns []sqlmodel.Column) []expectedColumn {
	shapes := make([]expectedColumn, 0, len(columns))
	for _, column := range columns {
		shapes = append(shapes, expectedColumn{
			name:       column.Name,
			engineName: column.SQLType.EngineName,
			nullable:   column.Nullable,
			hasDefault: column.HasDefault,
		})
	}
	return shapes
}

func TestCreateTableReadsMysqldumpColumnSpellings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		columns []expectedColumn
	}{
		{
			name: "character set, collation and zerofill",
			sql: "CREATE TABLE `a` (`id` bigint NOT NULL AUTO_INCREMENT, " +
				"`name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL, " +
				"`n` int unsigned zerofill NOT NULL, PRIMARY KEY (`id`))",
			columns: []expectedColumn{
				{name: "id", engineName: "bigint", nullable: false, hasDefault: true},
				{name: "name", engineName: "varchar", nullable: false, hasDefault: false},
				{name: "n", engineName: "int unsigned", nullable: false, hasDefault: false},
			},
		},
		{
			name: "charset shorthand, binary collation and signed",
			sql: "CREATE TABLE a (code char(3) CHARSET ascii BINARY NOT NULL, delta int SIGNED, " +
				"flag tinyint(1) ZEROFILL DEFAULT NULL)",
			columns: []expectedColumn{
				{name: "code", engineName: "char", nullable: false, hasDefault: false},
				{name: "delta", engineName: "int", nullable: true, hasDefault: false},
				{name: "flag", engineName: "tinyint unsigned", nullable: true, hasDefault: true},
			},
		},
		{
			name: "defaults, on update, comments and visibility",
			sql: "CREATE TABLE a (updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, " +
				"precise datetime(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3), " +
				"note text COMMENT 'a note', secret int INVISIBLE, fixed_width int COLUMN_FORMAT FIXED STORAGE DISK)",
			columns: []expectedColumn{
				{name: "updated_at", engineName: "timestamp", nullable: false, hasDefault: true},
				{name: "precise", engineName: "datetime", nullable: true, hasDefault: true},
				{name: "note", engineName: "text", nullable: true, hasDefault: false},
				{name: "secret", engineName: "int", nullable: true, hasDefault: false},
				{name: "fixed_width", engineName: "int", nullable: true, hasDefault: false},
			},
		},
		{
			name: "generated columns in both spellings",
			sql: "CREATE TABLE a (price int NOT NULL, doubled int GENERATED ALWAYS AS (price * 2) STORED, " +
				"halved int AS (price / 2) VIRTUAL NOT NULL, kept int AS (price) PERSISTENT)",
			columns: []expectedColumn{
				{name: "price", engineName: "int", nullable: false, hasDefault: false},
				{name: "doubled", engineName: "int", nullable: true, hasDefault: false},
				{name: "halved", engineName: "int", nullable: false, hasDefault: false},
				{name: "kept", engineName: "int", nullable: true, hasDefault: false},
			},
		},
		{
			name: "two-word type spellings and serial",
			sql: "CREATE TABLE a (id SERIAL, ratio double precision NOT NULL, label character varying(20), " +
				"code national char(2), body long varchar)",
			columns: []expectedColumn{
				{name: "id", engineName: "bigint unsigned", nullable: false, hasDefault: true},
				{name: "ratio", engineName: "double", nullable: false, hasDefault: false},
				{name: "label", engineName: "varchar", nullable: true, hasDefault: false},
				{name: "code", engineName: "char", nullable: true, hasDefault: false},
				{name: "body", engineName: "mediumtext", nullable: true, hasDefault: false},
			},
		},
		{
			name: "check constraints, key attributes and table indexes",
			sql: "CREATE TABLE a (id int KEY, qty int CONSTRAINT qty_positive CHECK (qty > 0) NOT ENFORCED, " +
				"email varchar(100) UNIQUE KEY, body text, " +
				"UNIQUE KEY email_unique USING BTREE (email) COMMENT 'unique', FULLTEXT KEY body_text (body) WITH PARSER ngram, " +
				"INDEX lowered ((lower(email))), CONSTRAINT qty_check CHECK (qty < 100) ENFORCED)",
			columns: []expectedColumn{
				{name: "id", engineName: "int", nullable: false, hasDefault: true},
				{name: "qty", engineName: "int", nullable: true, hasDefault: false},
				{name: "email", engineName: "varchar", nullable: true, hasDefault: false},
				{name: "body", engineName: "text", nullable: true, hasDefault: false},
			},
		},
		{
			name: "table options written by mysqldump",
			sql: "CREATE TABLE `a` (`id` int NOT NULL, PRIMARY KEY (`id`) USING BTREE) " +
				"ENGINE=InnoDB AUTO_INCREMENT=42 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci " +
				"ROW_FORMAT=DYNAMIC COMMENT='orders', STATS_PERSISTENT=0",
			columns: []expectedColumn{
				{name: "id", engineName: "int", nullable: false, hasDefault: false},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.Equal(t, testCase.columns, columnShapes(mutation.Columns))
		})
	}
}

func TestCreateTableRejectsAnElementWithoutAComma(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "unknown column attribute", sql: "CREATE TABLE a (id int NOT NULL BOGUS, name text)", token: "BOGUS"},
		{name: "missing comma between columns", sql: "CREATE TABLE a (id int name text)", token: "name"},
		{name: "unclosed column list", sql: "CREATE TABLE a (id int", token: ""},
		{name: "unknown table element", sql: "CREATE TABLE a (id int, CONSTRAINT c BOGUS (id))", token: "BOGUS"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])

			require.Nil(t, mutation)
			parseError, ok := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, ok, "got %v", err)
			require.Equal(t, testCase.token, parseError.Token)
		})
	}
}

func TestAFloatOfMoreThanFiftyThreeBitsIsRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		before string
		token  string
		after  string
	}{
		{name: "a float column", before: "CREATE TABLE t (c FLOAT(", token: "54", after: "))"},
		{name: "an unsigned FLOAT4 column", before: "CREATE TABLE t (c FLOAT4(", token: "60", after: ") UNSIGNED)"},
		{name: "a routine argument", before: "CREATE FUNCTION f(x FLOAT(", token: "54", after: ")) RETURNS int RETURN 1"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWith(t, New(), testCase.before+testCase.token+testCase.after)

			require.Nil(t, mutation)
			parseError, ok := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, ok, "got %v", err)
			assert.Equal(t, testCase.token, parseError.Token)
			assert.Equal(t, len(testCase.before), parseError.Position)
			assert.Equal(t, "FLOAT takes a precision of at most 53 bits", parseError.Message)
		})
	}
}

func TestAnEnumOrSetHoldsAtMostTheValuesMySQLAllows(t *testing.T) {
	t.Parallel()

	valueList := func(count int) string {
		values := make([]string, 0, count)
		for index := range count {
			values = append(values, fmt.Sprintf("'v%d'", index))
		}
		return strings.Join(values, ", ")
	}
	engine := New(WithMaxTokensPerStatement(dialectkit.MaxTokensPerStatementCeiling))

	testCases := []struct {
		name        string
		keyword     string
		values      int
		wantRefusal string
	}{
		{name: "an ENUM of the most values", keyword: "ENUM", values: maxEnumValues},
		{name: "an ENUM of one value too many", keyword: "ENUM", values: maxEnumValues + 1,
			wantRefusal: "an ENUM takes at most 65535 values"},
		{name: "a SET of the most members", keyword: "SET", values: maxSetMembers},
		{name: "a SET of one member too many", keyword: "SET", values: maxSetMembers + 1,
			wantRefusal: "a SET takes at most 64 members"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sql := "CREATE TABLE t (c " + testCase.keyword + "(" + valueList(testCase.values) + "))"

			mutation, err := applyWith(t, engine, sql)

			if testCase.wantRefusal == "" {
				require.NoError(t, err)
				require.Len(t, mutation.Columns, 1)
				assert.Len(t, mutation.Columns[0].SQLType.EnumValues, testCase.values)
				return
			}
			require.Nil(t, mutation)
			parseError, ok := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, ok, "got %v", err)
			firstRefused := fmt.Sprintf("'v%d'", testCase.values-1)
			assert.Equal(t, firstRefused, parseError.Token)
			assert.Equal(t, strings.Index(sql, firstRefused), parseError.Position)
			assert.Equal(t, testCase.wantRefusal, parseError.Message)
		})
	}
}

func TestAnEnumOrSetKeepsAHexadecimalOrBitValueAsMySQLReadsIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		columnType string
		wantValues []string
	}{
		{name: "every way of writing a value", columnType: "ENUM('a', X'62', 0x63, B'01100100', 0b01100101)",
			wantValues: []string{"a", "b", "c", "d", "e"}},
		{name: "lower-case prefixes", columnType: "SET(x'6162', b'0110001101100100')", wantValues: []string{"ab", "cd"}},
		{name: "an empty hexadecimal literal", columnType: "ENUM(X'', 'z')", wantValues: []string{"", "z"}},
		{name: "an odd number of hexadecimal digits after 0x", columnType: "ENUM(0x123)",
			wantValues: []string{"\x01\x23"}},
		{name: "bits that fill no whole byte", columnType: "ENUM(b'1', B'111111111')",
			wantValues: []string{"\x01", "\x01\xff"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, "CREATE TABLE t (c "+testCase.columnType+")")

			require.Len(t, mutation.Columns, 1)
			assert.Equal(t, testCase.wantValues, mutation.Columns[0].SQLType.EnumValues)
		})
	}
}

func TestAnEnumOrSetRefusesAMalformedHexadecimalOrBitValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		before  string
		token   string
		message string
	}{
		{name: "an odd number of digits in X'...'", before: "CREATE TABLE t (c ENUM('a', ", token: "X'616'",
			message: "a hexadecimal literal written X'...' takes an even number of digits"},
		{name: "a digit that is not hexadecimal", before: "CREATE TABLE t (c ENUM('a', ", token: "X'6G'",
			message: "expected the digits of a hexadecimal or bit literal"},
		{name: "a digit that is not a bit", before: "CREATE TABLE t (c SET('a', ", token: "B'102'",
			message: "expected the digits of a hexadecimal or bit literal"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWith(t, New(), testCase.before+testCase.token+"))")

			require.Nil(t, mutation)
			parseError, ok := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, ok, "got %v", err)
			assert.Equal(t, testCase.token, parseError.Token)
			assert.Equal(t, len(testCase.before), parseError.Position)
			assert.Equal(t, testCase.message, parseError.Message)
		})
	}
}

func TestQuotedReservedWordsAreColumnsInCreateTable(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE TABLE t (id INT PRIMARY KEY, `check` INT NOT NULL, `constraint` TEXT NOT NULL, "+
		"`primary` TEXT NOT NULL, `unique` INT NOT NULL, `key` TEXT, \"index\" INT NOT NULL)")

	names := make([]string, 0, len(mutation.Columns))
	for _, column := range mutation.Columns {
		names = append(names, column.Name)
	}
	require.Equal(t, []string{"id", "check", "constraint", "primary", "unique", "key", "index"}, names)
	require.Equal(t, []string{"id"}, mutation.PrimaryKey)
}

func TestInvisibleColumnsAreHidden(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE TABLE p (id INT PRIMARY KEY, secret VARCHAR(10) INVISIBLE, shown INT VISIBLE, note TEXT DEFAULT ('x') INVISIBLE);")

	hidden := map[string]bool{}
	for index := range mutation.Columns {
		hidden[mutation.Columns[index].Name] = mutation.Columns[index].Hidden
	}
	require.Equal(t, map[string]bool{"id": false, "secret": true, "shown": false, "note": true}, hidden)
}

func TestTextColumnsOfTheBinaryCharacterSetAreBinary(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE TABLE packed (a VARCHAR(10) CHARACTER SET binary, b TEXT CHARACTER SET binary NOT NULL, "+
		"c CHAR(4) CHARSET binary, d VARCHAR(5) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin, e LONGTEXT BINARY, "+
		"f MEDIUMTEXT CHARSET = binary)")

	categories := map[string]sqlmodel.SQLTypeCategory{}
	for _, column := range mutation.Columns {
		categories[column.Name] = column.SQLType.Category
	}
	require.Equal(t, map[string]sqlmodel.SQLTypeCategory{
		"a": sqlmodel.TypeCategoryBinary, "b": sqlmodel.TypeCategoryBinary, "c": sqlmodel.TypeCategoryBinary,
		"d": sqlmodel.TypeCategoryText, "e": sqlmodel.TypeCategoryText, "f": sqlmodel.TypeCategoryBinary,
	}, categories)
}

func TestCreateTableReadsATypeModifierAsTheNumberItWrites(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		columnType    string
		wantPrecision *int
		wantScale     *int
		wantLength    *int
	}{
		{name: "a precision and a scale", columnType: "DECIMAL(10, 2)", wantPrecision: new(10), wantScale: new(2)},
		{name: "a fraction is truncated", columnType: "DECIMAL(10.5, 2)", wantPrecision: new(10), wantScale: new(2)},
		{name: "an exponent is read as its value", columnType: "VARCHAR(1e3)", wantLength: new(1000)},
		{name: "a hexadecimal length", columnType: "VARCHAR(0x1F)", wantLength: new(31)},
		{name: "a binary length", columnType: "VARCHAR(0b101)", wantLength: new(5)},
		{name: "a length past the largest modifier is held at it", columnType: "VARCHAR(99999999999)",
			wantLength: new(dialectkit.MaxTypeModifier)},
		{name: "a fractional-second precision", columnType: "DATETIME(6)", wantPrecision: new(6)},
		{name: "a float whose width passes fifty-three bits with a scale", columnType: "FLOAT(60, 2)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWith(t, New(), "CREATE TABLE t (c "+testCase.columnType+")")
			require.NoError(t, err)
			require.Len(t, mutation.Columns, 1)

			sqlType := mutation.Columns[0].SQLType
			assert.Equal(t, testCase.wantPrecision, sqlType.Precision)
			assert.Equal(t, testCase.wantScale, sqlType.Scale)
			assert.Equal(t, testCase.wantLength, sqlType.Length)
		})
	}
}

func TestATypeModifierListMySQLDoesNotWriteIsRefused(t *testing.T) {
	t.Parallel()

	const hugeListLength = 20_000

	testCases := []struct {
		name    string
		before  string
		token   string
		after   string
		message string
	}{
		{name: "a third modifier", before: "CREATE TABLE t (c DECIMAL(10, 2, ", token: "3", after: "))",
			message: "a type takes at most 2 modifiers"},
		{name: "a huge modifier list is refused at its third modifier", before: "CREATE TABLE t (c DECIMAL(10, 2, ",
			token: "7", after: strings.Repeat(", 7", hugeListLength) + "))", message: "a type takes at most 2 modifiers"},
		{name: "a routine argument with a third modifier", before: "CREATE FUNCTION f(x DECIMAL(10, 2, ", token: "3",
			after: ")) RETURNS int RETURN 1", message: "a type takes at most 2 modifiers"},
		{name: "a word for a modifier", before: "CREATE TABLE t (c VARCHAR(", token: "max", after: "))",
			message: "expected a number as the type's modifier"},
		{name: "a signed modifier", before: "CREATE TABLE t (c DECIMAL(10, ", token: "-", after: "2))",
			message: "expected a number as the type's modifier"},
		{name: "a nested list", before: "CREATE TABLE t (c DECIMAL(10, ", token: "(", after: "2)))",
			message: "expected a number as the type's modifier"},
		{name: "an empty list", before: "CREATE TABLE t (c INT(", token: ")", after: ")",
			message: "expected a number as the type's modifier"},
		{name: "modifiers without a comma", before: "CREATE TABLE t (c DECIMAL(10 ", token: "2", after: "))",
			message: "expected ')' after the type's modifiers"},
		{name: "a list left open", before: "CREATE TABLE t (c VARCHAR", token: "(", after: "10",
			message: "unmatched parenthesis"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWith(t, New(), testCase.before+testCase.token+testCase.after)

			require.Nil(t, mutation)
			parseError, ok := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, ok, "got %v", err)
			assert.Equal(t, testCase.token, parseError.Token)
			assert.Equal(t, len(testCase.before), parseError.Position)
			assert.Equal(t, testCase.message, parseError.Message)
		})
	}
}
