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

import "strings"

// TypeAlias records a native spelling and its independent expected representation.
type TypeAlias struct {
	// Name is the native column declaration.
	Name string

	// Kind is the logical storage family.
	Kind string

	// Width is the integer or floating-point width in bits.
	Width int

	// Unsigned records the independent integer sign contract.
	Unsigned bool
}

// aliasGroup lists the type names an engine accepts for one representation.
type aliasGroup struct {
	// kind is the independent value family.
	kind string

	// names holds the accepted spellings in order.
	names []string

	// width is the representation width in bits, or zero for text.
	width int

	// unsigned records an unsigned integer representation.
	unsigned bool
}

// postgresAliasGroups lists the extra integer names of the PostgreSQL family.
var postgresAliasGroups = []aliasGroup{
	{kind: aliasInteger, width: aliasWidth16, unsigned: false, names: []string{"INT2"}},
	{kind: aliasInteger, width: aliasWidth32, unsigned: false, names: []string{"INT4"}},
	{kind: aliasInteger, width: aliasWidth64, unsigned: false, names: []string{"INT8"}},
}

// mysqlAliasGroups lists the extra integer names of the MySQL family.
var mysqlAliasGroups = []aliasGroup{
	{kind: aliasInteger, width: aliasWidth8, unsigned: false, names: []string{"INT1"}},
	{kind: aliasInteger, width: aliasWidth16, unsigned: false, names: []string{"INT2"}},
	{kind: aliasInteger, width: aliasWidth24, unsigned: false, names: []string{"INT3", "MIDDLEINT"}},
	{kind: aliasInteger, width: aliasWidth32, unsigned: false, names: []string{"INT4"}},
	{kind: aliasInteger, width: aliasWidth64, unsigned: false, names: []string{"INT8"}},
	{kind: aliasInteger, width: aliasWidth64, unsigned: true, names: []string{"INT8 UNSIGNED"}},
}

// engineAliasGroups lists the extra type names each engine accepts, in order.
var engineAliasGroups = map[string][]aliasGroup{
	enginePostgres:  postgresAliasGroups,
	engineTimescale: postgresAliasGroups,
	engineCockroach: postgresAliasGroups,
	engineMySQL:     mysqlAliasGroups,
	engineMariaDB:   mysqlAliasGroups,
	engineDuckDB: {
		{kind: aliasInteger, width: aliasWidth16, unsigned: false, names: []string{"INT2", "INT16", "SHORT"}},
		{kind: aliasInteger, width: aliasWidth32, unsigned: false, names: []string{"INT4", "INT32", "SIGNED"}},
		{kind: aliasInteger, width: aliasWidth64, unsigned: false, names: []string{"INT8", "INT64", "LONG"}},
		{kind: aliasInteger, width: aliasWidth128, unsigned: false, names: []string{"INT128"}},
		{kind: aliasInteger, width: aliasWidth8, unsigned: true, names: []string{"UINT8"}},
		{kind: aliasInteger, width: aliasWidth16, unsigned: true, names: []string{"UINT16"}},
		{kind: aliasInteger, width: aliasWidth32, unsigned: true, names: []string{"UINT32"}},
		{kind: aliasInteger, width: aliasWidth64, unsigned: true, names: []string{"UINT64"}},
		{kind: aliasInteger, width: aliasWidth128, unsigned: true, names: []string{"UINT128"}},
	},
	engineClickHouse: {
		{kind: aliasInteger, width: aliasWidth8, unsigned: false, names: []string{"TINYINT", "INT1", "BYTE", "TINYINT SIGNED", "INT1 SIGNED"}},
		{kind: aliasInteger, width: aliasWidth16, unsigned: false, names: []string{"SMALLINT SIGNED"}},
		{kind: aliasInteger, width: aliasWidth32, unsigned: false, names: []string{"INT", "MEDIUMINT", "MEDIUMINT SIGNED", "INT SIGNED", "INTEGER SIGNED"}},
		{kind: aliasInteger, width: aliasWidth64, unsigned: false, names: []string{"SIGNED", "BIGINT SIGNED", "BIGINT(20)"}},
		{kind: aliasInteger, width: aliasWidth8, unsigned: true, names: []string{"TINYINT UNSIGNED", "INT1 UNSIGNED"}},
		{kind: aliasInteger, width: aliasWidth16, unsigned: true, names: []string{"SMALLINT UNSIGNED", "YEAR"}},
		{kind: aliasInteger, width: aliasWidth32, unsigned: true, names: []string{"MEDIUMINT UNSIGNED", "INT UNSIGNED", "INTEGER UNSIGNED"}},
		{kind: aliasInteger, width: aliasWidth64, unsigned: true, names: []string{"UNSIGNED", "BIGINT UNSIGNED", "BIGINT(20) UNSIGNED", "BIT", "SET"}},
		{kind: typedFloat, width: aliasWidth32, unsigned: false, names: []string{"FLOAT", "REAL", "SINGLE"}},
		{kind: typedFloat, width: aliasWidth64, unsigned: false, names: []string{"DOUBLE"}},
		{kind: typedText, width: 0, unsigned: false, names: []string{"CHAR", "NCHAR", "TEXT", "BLOB", "BYTEA", "VARBINARY",
			"CHARACTER VARYING(7)", "NATIONAL CHARACTER LARGE OBJECT"}},
	},
}

// TypeAliases lists the type names an engine accepts with the representation each has.
//
// Takes engine (string) which names the dialect.
//
// Returns []TypeAlias which starts with the standard names and adds the engine's own.
func TypeAliases(engine string) []TypeAlias {
	integerWidth := aliasWidth32
	if engine == engineCockroach || engine == engineSQLite {
		integerWidth = aliasWidth64
	}
	smallWidth, bigWidth := aliasWidth16, aliasWidth64
	if engine == engineSQLite {
		smallWidth = aliasWidth64
	}
	aliases := []TypeAlias{{Name: "BIGINT", Kind: aliasInteger, Width: bigWidth, Unsigned: false}, {Name: "SMALLINT", Kind: aliasInteger, Width: smallWidth, Unsigned: false},
		{Name: "INTEGER", Kind: aliasInteger, Width: integerWidth, Unsigned: false}, {Name: "DOUBLE PRECISION", Kind: typedFloat, Width: aliasWidth64, Unsigned: false},
		{Name: "VARCHAR(7)", Kind: typedText, Width: 0, Unsigned: false}}
	for _, group := range engineAliasGroups[engine] {
		for _, name := range group.names {
			aliases = append(aliases, TypeAlias{Name: name, Kind: group.kind, Width: group.width, Unsigned: group.unsigned})
		}
	}
	return aliases
}

// AliasScenario creates a column probe for a native compatibility spelling.
//
// Takes engine (string) which selects table syntax.
// Takes alias (TypeAlias) which supplies the independent native spelling.
//
// Returns Scenario which exercises schema replay and result inference.
func AliasScenario(engine string, alias TypeAlias) Scenario {
	schema := "CREATE TABLE fuzz_alias (value " + alias.Name + ")"
	if engine == engineClickHouse {
		schema += " ENGINE = Memory"
	}
	literal := "1"
	if alias.Kind == typedText {
		literal = "'1'"
	}
	return Scenario{Engine: engine, Data: nil, Schema: []string{schema}, Inserts: []string{"INSERT INTO fuzz_alias (value) VALUES (" + literal + ")"},
		Queries: []ScenarioQuery{scenarioQuery("AliasValue", "SELECT value FROM fuzz_alias", nil, nil, 1, false)}, Relations: nil, Features: []string{"alias/" + strings.ToLower(alias.Name)}}
}
