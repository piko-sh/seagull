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

package fuzzschema

import (
	"slices"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

const (
	// enginePostgres names PostgreSQL.
	enginePostgres = "postgres"

	// engineCockroachDB names CockroachDB.
	engineCockroachDB = "cockroachdb"

	// engineTimescale names TimescaleDB.
	engineTimescale = "timescaledb"

	// engineMySQL names MySQL.
	engineMySQL = "mysql"

	// engineMariaDB names MariaDB.
	engineMariaDB = "mariadb"

	// engineSQLite names SQLite under either driver.
	engineSQLite = "sqlite"

	// engineDuckDB names DuckDB.
	engineDuckDB = "duckdb"

	// engineClickHouse names ClickHouse.
	engineClickHouse = "clickhouse"
)

const (
	// statusSupported marks a feature histories use.
	statusSupported = "supported"

	// statusUnsupported marks a feature the engine lacks.
	statusUnsupported = "unsupported"
)

const (
	// reasonNoAlterColumn explains SQLite's missing column alterations.
	reasonNoAlterColumn = "SQLite has no ALTER COLUMN, so a column's type, default or nullability changes only by rebuilding the table."

	// reasonCreateOnly explains constraints SQLite declares only in CREATE TABLE.
	reasonCreateOnly = "SQLite adds constraints only in CREATE TABLE."

	// reasonDuckDBCreateOnly explains constraints DuckDB declares only in CREATE TABLE.
	reasonDuckDBCreateOnly = "DuckDB has no ALTER TABLE ADD CONSTRAINT or DROP CONSTRAINT for this constraint."

	// reasonNoConstraint explains ClickHouse's missing relational constraints.
	reasonNoConstraint = "ClickHouse has no unique or foreign key constraints."

	// reasonFixedKey explains ClickHouse's fixed sorting key.
	reasonFixedKey = "A ClickHouse MergeTree table's primary key is its sorting key, fixed by CREATE TABLE."

	// reasonCockroachKey explains CockroachDB's mandatory primary key.
	reasonCockroachKey = "A CockroachDB table always has a primary key. ALTER PRIMARY KEY replaces it and keeps the old one as a unique index, which the model does not follow."

	// reasonNoSequence explains engines without sequences.
	reasonNoSequence = "The engine has no sequences."

	// reasonNoMaterialised explains engines without materialised views.
	reasonNoMaterialised = "The engine has no materialised views."

	// reasonNoEnumType explains engines whose enums are column types only.
	reasonNoEnumType = "The engine declares enums inline in a column type, with no enum type object."

	// reasonNoPartition explains engines without declarative partitions.
	reasonNoPartition = "The engine has no partitioned tables."

	// reasonPartitionNotTable explains engines whose partitions are not tables.
	reasonPartitionNotTable = "The engine's partitions are storage of one table, not tables, so none can be detached."

	// reasonNamespaceDatabase explains engines whose schemas are databases.
	reasonNamespaceDatabase = "A schema of this engine is a database outside the suite's isolated one, which the history cannot own, so it is not modelled."

	// reasonNoPosition explains engines without column position clauses.
	reasonNoPosition = "The engine appends every added column. It has no FIRST or AFTER clause."

	// reasonNoRedefine explains engines without MODIFY and CHANGE COLUMN.
	reasonNoRedefine = "Only MySQL and MariaDB redefine a whole column with MODIFY or CHANGE COLUMN."

	// reasonDropTableView explains engines whose DROP TABLE refuses views.
	reasonDropTableView = "Only ClickHouse drops a materialised view with DROP TABLE."

	// reasonOnlyClickHouse explains a ClickHouse spelling other engines lack.
	reasonOnlyClickHouse = "Only ClickHouse restates a whole column type with MODIFY COLUMN and keeps its default. Other engines' type changes are operation/alter-type."

	// reasonNoNamespace explains engines whose histories have one schema.
	reasonNoNamespace = "The engine's histories use one schema, so no foreign key crosses schemas."

	// reasonNoArray explains engines without array columns.
	reasonNoArray = "The engine has no array column type."

	// reasonNoZone explains engines without a zone-aware timestamp.
	reasonNoZone = "The engine has no zone-aware timestamp type; MySQL and MariaDB TIMESTAMP converts through the session zone but stores no offset."

	// reasonNoDefaultName explains engines whose unnamed constraints no statement drops by a
	// name.
	reasonNoDefaultName = "The engine gives an unnamed constraint no name a statement can drop it by, so histories name every constraint."

	// reasonNoUUID explains engines without a UUID type.
	reasonNoUUID = "The engine has no UUID type."
)

// unsupported maps each feature to the engines lacking it, with the reason.
var unsupported = map[string]map[string]string{
	featureAlterType: {engineSQLite: reasonNoAlterColumn,
		engineCockroachDB: "CockroachDB changes a column's type only behind the experimental enable_experimental_alter_column_type_general setting, outside a transaction."},
	featureSetDefault:   {engineSQLite: reasonNoAlterColumn},
	featureDropDefault:  {engineSQLite: reasonNoAlterColumn},
	featureSetNotNull:   {engineSQLite: reasonNoAlterColumn},
	featureDropNotNull:  {engineSQLite: reasonNoAlterColumn},
	featureAddCheck:     {engineSQLite: reasonCreateOnly, engineDuckDB: reasonDuckDBCreateOnly},
	featureDropCheck:    {engineSQLite: reasonCreateOnly, engineDuckDB: reasonDuckDBCreateOnly},
	featureCreateUnique: {engineClickHouse: reasonNoConstraint},
	featureAddUnique:    {engineClickHouse: reasonNoConstraint},
	featureDropUnique:   {engineClickHouse: reasonNoConstraint},
	featureAddPrimaryKey: {engineSQLite: reasonCreateOnly, engineClickHouse: reasonFixedKey, engineCockroachDB: reasonCockroachKey,
		engineDuckDB: reasonDuckDBCreateOnly},
	featureDropPrimaryKey: {engineSQLite: reasonCreateOnly, engineClickHouse: reasonFixedKey, engineCockroachDB: reasonCockroachKey,
		engineDuckDB: reasonDuckDBCreateOnly},
	featureCreateForeignKey: {engineClickHouse: reasonNoConstraint},
	featureAddForeignKey:    {engineSQLite: reasonCreateOnly, engineDuckDB: reasonDuckDBCreateOnly, engineClickHouse: reasonNoConstraint},
	featureDropForeignKey:   {engineSQLite: reasonCreateOnly, engineDuckDB: reasonDuckDBCreateOnly, engineClickHouse: reasonNoConstraint},
	featureSequence:         {engineMySQL: reasonNoSequence, engineSQLite: reasonNoSequence, engineClickHouse: reasonNoSequence},
	featureMaterialised: {engineMySQL: reasonNoMaterialised, engineMariaDB: reasonNoMaterialised, engineSQLite: reasonNoMaterialised,
		engineDuckDB: reasonNoMaterialised},
	featureAddGenerated: {engineDuckDB: "DuckDB declares generated columns only in CREATE TABLE."},
	featureEnumType: {engineSQLite: "SQLite has no enums.", engineMySQL: reasonNoEnumType, engineMariaDB: reasonNoEnumType,
		engineClickHouse: reasonNoEnumType},
	featureEnumValue: {engineSQLite: "SQLite has no enums.", engineMySQL: reasonNoEnumType, engineMariaDB: reasonNoEnumType,
		engineClickHouse: reasonNoEnumType, engineDuckDB: "DuckDB cannot add a value to an enum type."},
	featurePartition: {engineCockroachDB: "CockroachDB partitioning needs an enterprise licence.", engineDuckDB: reasonNoPartition,
		engineSQLite: reasonNoPartition},
	featureDetach: {engineMySQL: reasonPartitionNotTable, engineMariaDB: reasonPartitionNotTable, engineClickHouse: reasonPartitionNotTable,
		engineCockroachDB: reasonNoPartition, engineDuckDB: reasonNoPartition, engineSQLite: reasonNoPartition},
	featureNamespace: {engineSQLite: "A SQLite schema is an attached database file, which a migration cannot create.",
		engineMySQL: reasonNamespaceDatabase, engineMariaDB: reasonNamespaceDatabase, engineClickHouse: reasonNamespaceDatabase},
	featureDropNamespace: {engineSQLite: "A SQLite schema is an attached database file, which a migration cannot create.",
		engineMySQL: reasonNamespaceDatabase, engineMariaDB: reasonNamespaceDatabase, engineClickHouse: reasonNamespaceDatabase},
	featureRenameKeyed: {engineDuckDB: "DuckDB refuses to rename a column a key or index uses.",
		engineClickHouse: "ClickHouse has no unique or foreign keys, and refuses to rename a sorting key column."},
	featureRenameReferenced: {engineDuckDB: "DuckDB refuses to alter a table another table's foreign key references.",
		engineClickHouse: reasonNoConstraint},
	featureDropMaterialisedTable: {enginePostgres: reasonDropTableView, engineCockroachDB: reasonDropTableView, engineTimescale: reasonDropTableView,
		engineMySQL: reasonDropTableView, engineMariaDB: reasonDropTableView, engineSQLite: reasonDropTableView, engineDuckDB: reasonDropTableView},
	featureCrossSchemaKey: {engineSQLite: reasonNoNamespace, engineMySQL: reasonNoNamespace, engineMariaDB: reasonNoNamespace,
		engineClickHouse: reasonNoNamespace, engineDuckDB: "DuckDB keeps a foreign key within its table's schema."},
	featureModifyDefaulted: {enginePostgres: reasonOnlyClickHouse, engineCockroachDB: reasonOnlyClickHouse, engineTimescale: reasonOnlyClickHouse,
		engineMySQL: reasonOnlyClickHouse, engineMariaDB: reasonOnlyClickHouse, engineSQLite: reasonOnlyClickHouse, engineDuckDB: reasonOnlyClickHouse},
	featurePosition: {enginePostgres: reasonNoPosition, engineCockroachDB: reasonNoPosition, engineTimescale: reasonNoPosition,
		engineSQLite: reasonNoPosition, engineDuckDB: reasonNoPosition},
	featureRedefine: {enginePostgres: reasonNoRedefine, engineCockroachDB: reasonNoRedefine, engineTimescale: reasonNoRedefine,
		engineSQLite: reasonNoRedefine, engineDuckDB: reasonNoRedefine, engineClickHouse: reasonNoRedefine},
	featureDefaultName:         {engineSQLite: reasonNoDefaultName, engineDuckDB: reasonNoDefaultName, engineClickHouse: reasonNoDefaultName},
	featureColumnConstraint:    {engineSQLite: reasonNoDefaultName, engineDuckDB: reasonNoDefaultName, engineClickHouse: reasonNoDefaultName},
	familyFeature(FamilyUUID):  {engineSQLite: reasonNoUUID, engineMySQL: reasonNoUUID},
	familyFeature(FamilyArray): {engineSQLite: reasonNoArray, engineMySQL: reasonNoArray, engineMariaDB: reasonNoArray},
	familyFeature(FamilyTimestamp): {
		engineClickHouse: "A ClickHouse DateTime64 stores an instant, shown in the column's or the server's zone, so it is always zone-aware."},
	familyFeature(FamilyTimestampTZ): {engineSQLite: "SQLite has no zone-aware timestamp type.", engineMySQL: reasonNoZone,
		engineMariaDB: reasonNoZone},
	familyFeature(FamilyJSON): {engineSQLite: "SQLite stores JSON as TEXT; it has no JSON type.",
		engineMariaDB:    "MariaDB JSON is LONGTEXT with an implicit json_valid CHECK, so its catalogue shows text.",
		engineClickHouse: "The ClickHouse JSON type changed shape across 24.x and 25.x releases, so it is not modelled."},
	familyFeature(FamilyBinary): {engineClickHouse: "ClickHouse String holds bytes and text alike, with no separate binary type."},
	familyFeature(FamilyEnum):   {engineSQLite: "SQLite has no enums."},
}

// allFeatures lists every feature a history can use, in a stable order.
var allFeatures = []string{featureCreateTable, featureDropTable, featureRecreate, featureRenameTable, featureAddColumn,
	featurePosition, featureModifyDefaulted, featureDropColumn, featureRenameColumn, featureRenameKeyed, featureRenameReferenced, featureCrossSchemaKey,
	featureDropMaterialisedTable, featureRedefine, featureAlterType, featureSetDefault,
	featureDropDefault, featureSetNotNull, featureDropNotNull, featureCreateCheck, featureAddCheck, featureDropCheck,
	featureCreateUnique, featureAddUnique, featureDropUnique, featureCreatePrimaryKey, featureAddPrimaryKey,
	featureDropPrimaryKey, featureCreateForeignKey, featureAddForeignKey, featureDropForeignKey, featureSequence,
	featureView, featureMaterialised, featureCreateGenerated, featureAddGenerated, featureEnumType, featureEnumValue,
	featurePartition, featureDetach, featureNamespace, featureDropNamespace, featureDefaultName, featureColumnConstraint}

// Supported reports whether the package models an engine.
//
// Takes engine (string) which names the dialect.
//
// Returns bool which is true for the eight modelled engines.
func Supported(engine string) bool {
	return slices.Contains([]string{enginePostgres, engineCockroachDB, engineTimescale, engineMySQL, engineMariaDB,
		engineSQLite, engineDuckDB, engineClickHouse}, engine)
}

// Capabilities lists every feature with whether the engine's histories use it, and the
// reason for each feature they leave out.
//
// Takes engine (string) which names the dialect.
//
// Returns []fuzzcase.Capability which records supported, unsupported and steered
// features.
func Capabilities(engine string) []fuzzcase.Capability {
	if !Supported(engine) {
		return nil
	}
	features := slices.Clone(allFeatures)
	for _, family := range allFamilies {
		features = append(features, familyFeature(family))
	}
	result := make([]fuzzcase.Capability, 0, len(features))
	for _, feature := range features {
		capability := fuzzcase.Capability{Feature: feature, NativeType: "", CastType: "", Status: statusSupported, Reason: ""}
		if reason, ok := unsupported[feature][engine]; ok {
			capability.Status, capability.Reason = statusUnsupported, reason
		}
		result = append(result, capability)
	}
	return result
}

// supports reports whether the engine has a feature.
//
// Takes engine (string) which names the dialect.
// Takes feature (string) which names the feature.
//
// Returns bool which is false when the engine lacks the feature.
func supports(engine, feature string) bool {
	_, missing := unsupported[feature][engine]
	return !missing
}

// familyFeature names the coverage feature of a type family.
//
// Takes family (string) which is a Family constant.
//
// Returns string which is the feature name.
func familyFeature(family string) string {
	return "family/" + family
}
