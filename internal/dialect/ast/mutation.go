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

package ast

import (
	"fmt"

	"seagull.sh/seagull/internal/sqlmodel"
)

// CopiedTable is a table whose columns a CREATE TABLE copies, as a LIKE element does.
type CopiedTable struct {
	// Table names the table whose columns are copied.
	Table TableReference

	// Position is how many of the mutation's own Columns come before the copied columns.
	Position int

	// Keys includes the source table's primary key and constraints when copying columns, as
	// MySQL CREATE TABLE LIKE and LIKE INCLUDING INDEXES do.
	Keys bool
}

// Placement says where a column or an enum value goes among its siblings.
type Placement struct {
	// Anchor names the column or value a PlacementBefore or PlacementAfter is relative to.
	Anchor string

	// Kind says where the column or value goes.
	Kind PlacementKind
}

// PlacementKind says where a column or an enum value goes among its siblings.
type PlacementKind uint8

const (
	// PlacementDefault puts an added column or value last and leaves an altered column where
	// it is.
	PlacementDefault PlacementKind = iota

	// PlacementFirst puts the column or value first, as FIRST does.
	PlacementFirst

	// PlacementBefore puts the column or value just before Placement.Anchor, as BEFORE does.
	PlacementBefore

	// PlacementAfter puts the column or value just after Placement.Anchor, as AFTER does.
	PlacementAfter
)

// ObjectKind names the kind of catalogue object a mutation is restricted to.
type ObjectKind uint8

const (
	// ObjectKindAny is the zero value: the mutation applies to whatever object its kind
	// addresses.
	ObjectKindAny ObjectKind = iota

	// ObjectKindTable restricts the mutation to a table.
	ObjectKindTable

	// ObjectKindView restricts the mutation to a view.
	ObjectKindView

	// ObjectKindSequence restricts the mutation to a sequence.
	ObjectKindSequence
)

const (
	// MutationCreateTable creates a new table.
	MutationCreateTable MutationKind = iota

	// MutationDropTable removes a table.
	MutationDropTable

	// MutationAlterTableAddColumn adds a column to a table.
	MutationAlterTableAddColumn

	// MutationAlterTableDropColumn removes a column from a table.
	MutationAlterTableDropColumn

	// MutationAlterTableAlterColumn modifies a column definition.
	MutationAlterTableAlterColumn

	// MutationAlterTableRenameColumn renames a column.
	MutationAlterTableRenameColumn

	// MutationAlterTableRenameTable renames the table or view TableName names to NewName.
	MutationAlterTableRenameTable

	// MutationAlterTableSetSchema moves the table or view TableName names to the schema
	// NewName names.
	MutationAlterTableSetSchema

	// MutationCreateEnum creates an enum type.
	MutationCreateEnum

	// MutationAlterEnumAddValue adds a value to an enum type.
	MutationAlterEnumAddValue

	// MutationAlterEnumRenameValue renames a value in an enum type.
	MutationAlterEnumRenameValue

	// MutationDropEnum removes an enum type.
	MutationDropEnum

	// MutationCreateCompositeType creates a composite type.
	MutationCreateCompositeType

	// MutationDropType removes a type.
	MutationDropType

	// MutationCreateFunction creates a function.
	MutationCreateFunction

	// MutationDropFunction removes a function.
	MutationDropFunction

	// MutationCreateSchema creates a schema.
	MutationCreateSchema

	// MutationDropSchema removes a schema.
	MutationDropSchema

	// MutationCreateView creates a view.
	MutationCreateView

	// MutationDropView removes a view.
	MutationDropView

	// MutationCreateIndex creates an index.
	MutationCreateIndex

	// MutationDropIndex removes an index.
	MutationDropIndex

	// MutationCreateExtension loads an extension.
	MutationCreateExtension

	// MutationComment sets a comment on a database object.
	MutationComment

	// MutationCreateTrigger creates a trigger.
	MutationCreateTrigger

	// MutationDropTrigger removes a trigger.
	MutationDropTrigger

	// MutationAlterTableAddConstraint adds a constraint to a table.
	MutationAlterTableAddConstraint

	// MutationAlterTableDropConstraint removes a constraint from a table.
	MutationAlterTableDropConstraint

	// MutationCreateSequence creates a sequence.
	MutationCreateSequence

	// MutationDropSequence removes a sequence.
	MutationDropSequence

	// MutationAsyncDataUpdate records a data-mutating ALTER TABLE ... UPDATE statement
	// (ClickHouse).
	//
	// The mutation does not change the catalogue's schema view; it carries the WHERE/SET
	// text in EngineSpecific so the migration runner can execute it and codegen can ignore
	// it.
	MutationAsyncDataUpdate

	// MutationAsyncDataDelete records a data-mutating ALTER TABLE ... DELETE statement
	// (ClickHouse).
	//
	// See MutationAsyncDataUpdate for the storage convention.
	MutationAsyncDataDelete

	// MutationCreateDictionary creates a ClickHouse dictionary (key-value lookup table). The
	// dictionary is catalogue-visible as a view-like entity; its columns plus structured
	// engine metadata (PRIMARY KEY, SOURCE, LAYOUT, LIFETIME) are captured on the mutation.
	MutationCreateDictionary

	// MutationDropDictionary removes a ClickHouse dictionary.
	MutationDropDictionary

	// MutationExchangeTables atomically swaps two ClickHouse tables. The primary mutation
	// carries the left table; the right table name is stored under the EngineSpecific key
	// EXCHANGE_TARGET so downstream consumers can read both sides.
	MutationExchangeTables

	// MutationAlterTableAddProjection adds a projection to a table (ClickHouse). The
	// projection name is carried under EngineSpecific[PROJECTION_NAME] and the SELECT body
	// under EngineSpecific[PROJECTION_SELECT].
	MutationAlterTableAddProjection

	// MutationAlterTableDropProjection removes a projection from a table. The projection
	// name is carried under EngineSpecific[PROJECTION_NAME].
	MutationAlterTableDropProjection

	// MutationAlterTableMaterializeProjection rebuilds a projection's data on disk; pure
	// runtime operation captured for migration-runner replay.
	MutationAlterTableMaterializeProjection

	// MutationAlterTableAddSkippingIndex adds a data-skipping index to a table.
	// EngineSpecific carries INDEX_NAME, INDEX_EXPR, INDEX_TYPE, INDEX_GRANULARITY.
	MutationAlterTableAddSkippingIndex

	// MutationAlterTableDropSkippingIndex removes a data-skipping index. EngineSpecific
	// carries INDEX_NAME.
	MutationAlterTableDropSkippingIndex

	// MutationAlterTableMaterializeIndex rebuilds a data-skipping index; pure runtime
	// operation captured for migration-runner replay.
	MutationAlterTableMaterializeIndex

	// MutationAlterTableAddStatistics adds column statistics to a table. EngineSpecific
	// carries STATS_COLUMNS, STATS_TYPES.
	MutationAlterTableAddStatistics

	// MutationAlterTableDropStatistics removes column statistics from a table.
	// EngineSpecific carries STATS_COLUMNS.
	MutationAlterTableDropStatistics

	// MutationAlterTableMaterializeStatistics rebuilds column statistics; pure runtime
	// operation captured for migration-runner replay.
	MutationAlterTableMaterializeStatistics

	// MutationAlterTableModifyStatistics modifies column statistics. EngineSpecific carries
	// STATS_COLUMNS, STATS_TYPES.
	MutationAlterTableModifyStatistics

	// MutationAlterTableMaterializeColumn rebuilds a materialised column's stored data; pure
	// runtime operation captured for migration-runner replay.
	MutationAlterTableMaterializeColumn

	// MutationAlterTableModifyColumn captures sub-target column modifications (REMOVE
	// DEFAULT, MODIFY COMMENT, RESET SETTING) that do not change the column's type.
	// EngineSpecific carries COLUMN_REMOVE, COLUMN_MODIFY_COMMENT, or COLUMN_RESET_SETTING.
	MutationAlterTableModifyColumn

	// MutationAlterTableModifyQuery captures a refreshable materialised view's body
	// replacement (ALTER TABLE v MODIFY QUERY). EngineSpecific[NEW_QUERY] carries the new
	// SELECT text.
	MutationAlterTableModifyQuery

	// MutationAlterTableModifyRefresh captures changes to a refreshable materialised view's
	// refresh policy (ALTER TABLE v MODIFY REFRESH / SQL SECURITY / DEFINER).
	MutationAlterTableModifyRefresh

	// MutationAlterTablePartition captures partition operations on a MergeTree table
	// (ATTACH/DETACH/DROP/MOVE/REPLACE/FETCH/FREEZE/ UNFREEZE PARTITION). EngineSpecific
	// carries PARTITION_OP, PARTITION_TARGET, PARTITION_EXPR, and optional PARTITION_DEST,
	// PARTITION_BACKUP_NAME, PARTITION_FROM_TABLE, PARTITION_DETACHED.
	MutationAlterTablePartition

	// MutationCreateUser captures a CREATE USER statement (RBAC). EngineSpecific[USER_NAME]
	// carries the user name; the rest of the statement text is captured opaquely under
	// EngineSpecific[STATEMENT_BODY].
	MutationCreateUser

	// MutationAlterUser captures an ALTER USER statement (RBAC).
	MutationAlterUser

	// MutationDropUser captures a DROP USER statement (RBAC).
	MutationDropUser

	// MutationCreateRole captures a CREATE ROLE statement (RBAC).
	MutationCreateRole

	// MutationAlterRole captures an ALTER ROLE statement (RBAC).
	MutationAlterRole

	// MutationDropRole captures a DROP ROLE statement (RBAC).
	MutationDropRole

	// MutationCreatePolicy captures a CREATE ROW POLICY statement (RBAC).
	MutationCreatePolicy

	// MutationAlterPolicy captures an ALTER POLICY statement (RBAC).
	MutationAlterPolicy

	// MutationDropPolicy captures a DROP POLICY statement (RBAC).
	MutationDropPolicy

	// MutationCreateQuota captures a CREATE QUOTA statement (RBAC).
	MutationCreateQuota

	// MutationAlterQuota captures an ALTER QUOTA statement (RBAC).
	MutationAlterQuota

	// MutationDropQuota captures a DROP QUOTA statement (RBAC).
	MutationDropQuota

	// MutationCreateSettingsProfile captures a CREATE SETTINGS PROFILE statement (RBAC).
	MutationCreateSettingsProfile

	// MutationAlterSettingsProfile captures an ALTER SETTINGS PROFILE statement (RBAC).
	MutationAlterSettingsProfile

	// MutationDropSettingsProfile captures a DROP SETTINGS PROFILE statement (RBAC).
	MutationDropSettingsProfile

	// MutationGrantManagement captures a GRANT or REVOKE statement (RBAC).
	// EngineSpecific[RBAC_KIND] is GRANT or REVOKE; the body text is under
	// EngineSpecific[STATEMENT_BODY].
	MutationGrantManagement

	// MutationAttachTable captures an ATTACH TABLE statement, which re-registers an existing
	// on-disk table with the server.
	MutationAttachTable

	// MutationDetachTable captures a DETACH TABLE statement, which unregisters a table
	// without removing its data.
	MutationDetachTable

	// MutationBackup captures a BACKUP statement. The body text is under
	// EngineSpecific[STATEMENT_BODY].
	MutationBackup

	// MutationRestore captures a RESTORE statement. The body text is under
	// EngineSpecific[STATEMENT_BODY].
	MutationRestore

	// MutationKillQuery captures a KILL QUERY statement.
	MutationKillQuery

	// MutationKillMutation captures a KILL MUTATION statement.
	MutationKillMutation

	// MutationCreateDomain captures a CREATE DOMAIN statement: a named type over a base
	// type. EnumName holds the domain's name, and the single column of Columns holds the
	// base type, its array dimensions and, as Nullable false, a NOT NULL constraint.
	MutationCreateDomain

	// MutationAttachPartition makes an existing table a partition of the table PartitionOf
	// names, as ALTER TABLE parent ATTACH PARTITION does. SchemaName and TableName name the
	// table, which from then on follows its parent's column changes.
	MutationAttachPartition

	// MutationDetachPartition makes a partition of the table PartitionOf names a table of
	// its own again, as ALTER TABLE parent DETACH PARTITION does. SchemaName and TableName
	// name the partition, which keeps the columns it has.
	MutationDetachPartition

	// MutationAlterTypeRename renames the enum, composite type or domain named by EnumName
	// to NewName and updates its dependent columns and types.
	MutationAlterTypeRename

	// MutationAlterTypeSetSchema moves the enum, composite type or domain named by EnumName
	// to the schema named by NewName and updates its dependent columns and types.
	MutationAlterTypeSetSchema

	// MutationSetSearchPath sets the search path the rest of the migrations replay with, as
	// SET search_path does: SearchPath lists its schemas, in order. Unqualified names a
	// later statement creates or reads follow it; Local marks one that lasts until the end
	// of the migration that sets it.
	MutationSetSearchPath

	// MutationResetSearchPath restores the search path the migrations started with, as SET
	// search_path TO DEFAULT and RESET search_path do; Local marks one that lasts until the
	// end of the migration that resets it.
	MutationResetSearchPath

	// mutationKindCount is one past the last declared kind; IsValid compares with it.
	mutationKindCount
)

// String names the object kind, such as "sequence", or gives the number of a value that
// is not a declared kind.
//
// Returns string which is the name.
func (kind ObjectKind) String() string {
	switch kind {
	case ObjectKindAny:
		return "any"
	case ObjectKindTable:
		return "table"
	case ObjectKindView:
		return "view"
	case ObjectKindSequence:
		return "sequence"
	default:
		return fmt.Sprintf("ObjectKind(%d)", uint8(kind))
	}
}

// CatalogueMutationOption sets an optional attribute of a mutation built by
// NewCatalogueMutation.
type CatalogueMutationOption func(*CatalogueMutation)

// MutationKind identifies the type of DDL mutation.
type MutationKind uint8

// IsValid reports whether the kind is one the ast package declares, so a consumer can
// reject a mutation whose kind it cannot know.
//
// Returns bool which is true for a declared kind.
func (kind MutationKind) IsValid() bool {
	return kind < mutationKindCount
}

// CatalogueMutation describes a single DDL change to the schema catalogue.
type CatalogueMutation struct {
	// Function holds the function a CREATE FUNCTION statement defines, or the function a
	// DROP FUNCTION statement names.
	Function *FunctionDefinition

	// ViewDefinition holds the SELECT body used to derive columns for CREATE VIEW or CREATE
	// TABLE AS SELECT.
	ViewDefinition *RawQueryAnalysis

	// ConstraintName is the constraint affected by DROP CONSTRAINT.
	ConstraintName string

	// TriggerName is the trigger affected, if applicable.
	TriggerName string

	// ColumnName is the specific column affected by ALTER COLUMN or DROP COLUMN.
	ColumnName string

	// DroppedColumnPrefix, on a MutationAlterTableDropColumn, makes the drop take every
	// column whose name starts with it as well as ColumnName, as dropping a ClickHouse
	// Nested column n drops the n.x and n.y columns the engine stores it as. Empty for a
	// drop of ColumnName alone.
	DroppedColumnPrefix string

	// EnumName is the enum type affected, if applicable.
	EnumName string

	// SchemaName is the schema affected by the mutation.
	SchemaName string

	// TableName is the table affected, if applicable.
	TableName string

	// OwnedByColumn is the column that owns a sequence.
	OwnedByColumn string

	// VirtualModuleName is the module name from CREATE VIRTUAL TABLE ... USING module(...),
	// or empty for non-virtual tables.
	VirtualModuleName string

	// OwnedByTable is the table that owns a sequence (OWNED BY table.column).
	OwnedByTable string

	// SequenceName is the sequence affected, if applicable.
	SequenceName string

	// NewName holds the target name for RENAME operations.
	NewName string

	// Comment holds the comment a MutationComment sets on the table, view, column or enum it
	// names, an empty one removing it, as COMMENT ON ... IS NULL does, or the comment a
	// MutationCreateTable gives its table, such as MySQL's COMMENT table option.
	Comment string

	// PrimaryKey holds primary key column names for CREATE TABLE.
	PrimaryKey []string

	// Constraints holds table constraints for CREATE TABLE or ADD CONSTRAINT.
	Constraints []sqlmodel.Constraint

	// Columns holds column definitions for CREATE TABLE or ADD COLUMN.
	Columns []sqlmodel.Column

	// AdditionalMutations carries follow-up mutations produced by a single DDL statement
	// that the engine could not collapse into the primary mutation.
	//
	// The catalogue builder applies these in order immediately after the primary, with the
	// same Origin. Used for multi-action ALTER TABLE statements where a single statement
	// adds a column and also adds a constraint, for example.
	AdditionalMutations []*CatalogueMutation

	// EngineSpecific holds mutation details defined by the dialect. The core analyser does
	// not interpret them.
	EngineSpecific map[string]string

	// InheritsTables lists parent tables from an INHERITS clause on CREATE TABLE. The child
	// takes each parent's columns ahead of its own, a column of the same name in more than
	// one place merged into one, and later changes to a parent's columns reach it.
	InheritsTables []TableReference

	// CopiedTables lists tables whose columns CREATE TABLE copies and where those columns
	// fit among the mutation's own Columns. Each copy uses the source columns at execution
	// time and ignores later changes.
	CopiedTables []CopiedTable

	// Placement says where an ADD COLUMN puts its columns, where an ALTER COLUMN moves its
	// column, or where an ADD VALUE puts its enum value. The zero value puts an added column
	// or value last and leaves an altered column where it is.
	Placement Placement

	// PartitionOf identifies the parent table of a partition mutation. It is nil for
	// mutations that do not create, attach or detach a partition.
	PartitionOf *TableReference

	// EnumValues holds enum values for CREATE TYPE or ADD VALUE.
	EnumValues []string

	// SearchPath lists, in order, the schemas a MutationSetSearchPath puts on the search
	// path, by their canonical names.
	SearchPath []string

	// UnanalysedParts lists the parts of the statement the dialect could not analyse, such
	// as a view body whose columns it could not work out, each with a message saying what
	// was skipped and the span of the part.
	//
	// The catalogue builder reports each one at the statement, so the mutation still applies
	// but the gap is not silent.
	UnanalysedParts []Warning

	// Origin records which migration file produced this mutation. Set by the catalogue
	// builder before applying each mutation.
	Origin sqlmodel.MigrationOrigin

	// Kind identifies the type of mutation (create table, alter column, etc.).
	Kind MutationKind

	// TargetKind restricts the kind of object a rename, move or drop applies to, such as
	// ObjectKindSequence for ALTER SEQUENCE, so the catalogue builder changes only an object
	// of that kind and reports a missing one rather than changing another object of the same
	// name.
	//
	// The zero value, ObjectKindAny, applies to whatever object Kind addresses.
	TargetKind ObjectKind

	// IsWithoutRowID indicates the table was created with WITHOUT ROWID.
	IsWithoutRowID bool

	// IsVirtual indicates the table was created with CREATE VIRTUAL TABLE.
	IsVirtual bool

	// SetsNotNull marks an ALTER COLUMN that makes ColumnName NOT NULL and changes nothing
	// else about it, such as ALTER COLUMN c SET NOT NULL. Such a mutation has no Columns.
	SetsNotNull bool

	// DropsNotNull marks an ALTER COLUMN that lets ColumnName hold NULL and changes nothing
	// else about it, such as ALTER COLUMN c DROP NOT NULL. Such a mutation has no Columns.
	DropsNotNull bool

	// SetsType marks an ALTER COLUMN that changes only the type of ColumnName, such as ALTER
	// COLUMN c TYPE bigint, keeping its nullability, default and every other attribute. The
	// single column of Columns carries the new type and its array dimensions.
	SetsType bool

	// IfNotExists marks a statement written with IF NOT EXISTS: a CREATE of an object that
	// already exists changes nothing, and an ADD COLUMN or ADD VALUE skips each column or
	// value that is already there.
	IfNotExists bool

	// IfExists marks a statement that alters or drops its table, view or type IF EXISTS:
	// when that object is missing, nothing changes.
	IfExists bool

	// IfColumnExists marks an action on one column written with IF EXISTS on the column,
	// such as MODIFY COLUMN IF EXISTS: when ColumnName is missing, nothing changes.
	IfColumnExists bool

	// Only marks an ALTER TABLE written with ONLY: the change applies to the named table
	// alone and does not reach the tables that inherit its columns.
	Only bool

	// Local marks a MutationSetSearchPath or MutationResetSearchPath that lasts until the
	// end of the migration that makes it, the transaction the migration runs in, as SET
	// LOCAL does.
	Local bool

	// ReplacesHiddenKey marks a mutation giving a table a primary key that replaces a key
	// the database supplied: when the table is keyed by one hidden column, that column is
	// dropped, as CockroachDB drops the rowid it keys a table created without a key by.
	ReplacesHiddenKey bool
}

// NewCatalogueMutation returns a mutation of the given kind against a schema and table,
// with every other attribute unset unless an option sets it.
//
// Takes kind (MutationKind) which is the operation the mutation applies.
// Takes schemaName (string) which is the schema it targets, or "" for the default.
// Takes tableName (string) which is the table it targets, or "" when it targets none.
// Takes options (...CatalogueMutationOption) which set any further attributes.
//
// Returns *CatalogueMutation which is the configured mutation.
func NewCatalogueMutation(
	kind MutationKind,
	schemaName string,
	tableName string,
	options ...CatalogueMutationOption,
) *CatalogueMutation {
	mutation := &CatalogueMutation{}
	mutation.Kind = kind
	mutation.SchemaName = schemaName
	mutation.TableName = tableName
	for _, option := range options {
		option(mutation)
	}
	return mutation
}

// FunctionDefinition is the input form of a function: the signature the catalogue
// records, plus the parsed body the catalogue builder analyses to infer what the
// signature leaves open, such as the return type or whether the function writes data.
//
// The body never reaches the catalogue: the builder stores only the embedded Function.
type FunctionDefinition struct {
	// Function is the signature the catalogue records for the function.
	*sqlmodel.Function

	// Body is the parsed expression tree of a function whose body fits in a single
	// expression, such as a ClickHouse lambda. Nil when the dialect leaves the body as text
	// in Function.BodySQL or does not capture it.
	Body Expression

	// BodyParameters lists the parameter names Body refers to, in declaration order, so the
	// analyser can bind them before resolving Body.
	BodyParameters []string
}

// NewFunctionDefinition returns the definition of a function with no parsed body.
//
// Takes function (*sqlmodel.Function) which is the function's signature.
//
// Returns *FunctionDefinition which carries only the signature.
func NewFunctionDefinition(function *sqlmodel.Function) *FunctionDefinition {
	return &FunctionDefinition{
		Function:       function,
		Body:           nil,
		BodyParameters: nil,
	}
}

// NewFunctionReference returns a definition that identifies a function by schema and name
// alone, with no arguments and a zero return type, for statements that name a function
// without declaring its full shape.
//
// Takes schemaName (string) which is the schema the function belongs to, or "" for the
// default.
// Takes functionName (string) which is the function's name.
//
// Returns *FunctionDefinition which carries only the schema and the name.
func NewFunctionReference(schemaName string, functionName string) *FunctionDefinition {
	function := sqlmodel.NewFunction(
		nil,
		sqlmodel.SQLType{},
		sqlmodel.FunctionNullableCalledOnNull,
		sqlmodel.WithFunctionName(functionName),
	)
	function.Schema = schemaName
	return NewFunctionDefinition(function)
}

// WithEngineSpecific attaches engine-specific attributes to a mutation.
//
// Takes engineSpecific (map[string]string) which holds the attributes.
//
// Returns CatalogueMutationOption which sets EngineSpecific.
func WithEngineSpecific(engineSpecific map[string]string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.EngineSpecific = engineSpecific
	}
}

// WithNewName sets the name a renaming mutation gives its target.
//
// Takes newName (string) which is the new name.
//
// Returns CatalogueMutationOption which sets NewName.
func WithNewName(newName string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.NewName = newName
	}
}

// WithIfNotExists marks a mutation as written with IF NOT EXISTS.
//
// Takes ifNotExists (bool) which is true when the statement said IF NOT EXISTS.
//
// Returns CatalogueMutationOption which sets IfNotExists.
func WithIfNotExists(ifNotExists bool) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.IfNotExists = ifNotExists
	}
}

// WithIfExists marks a mutation as altering or dropping its object IF EXISTS.
//
// Takes ifExists (bool) which is true when the statement said IF EXISTS.
//
// Returns CatalogueMutationOption which sets IfExists.
func WithIfExists(ifExists bool) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.IfExists = ifExists
	}
}

// WithPlacement sets where a mutation puts the column or enum value it adds or moves.
//
// Takes kind (PlacementKind) which says where it goes.
// Takes anchor (string) which names the column or value a BEFORE or AFTER placement is
// relative to, or is empty.
//
// Returns CatalogueMutationOption which sets Placement.
func WithPlacement(kind PlacementKind, anchor string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.Placement = Placement{Anchor: anchor, Kind: kind}
	}
}

// WithCopiedTables sets the tables whose columns a created table copies.
//
// Takes copied ([]CopiedTable) which are the tables, in order.
//
// Returns CatalogueMutationOption which sets CopiedTables.
func WithCopiedTables(copied []CopiedTable) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.CopiedTables = copied
	}
}

// WithColumnName sets the column a mutation targets.
//
// Takes columnName (string) which is the column's name.
//
// Returns CatalogueMutationOption which sets ColumnName.
func WithColumnName(columnName string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.ColumnName = columnName
	}
}

// WithDroppedColumnPrefix makes a DROP COLUMN mutation also drop every column whose name
// starts with the prefix.
//
// Takes prefix (string) which is the prefix of the further columns to drop.
//
// Returns CatalogueMutationOption which sets DroppedColumnPrefix.
func WithDroppedColumnPrefix(prefix string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.DroppedColumnPrefix = prefix
	}
}

// WithColumns sets the column definitions a mutation adds or replaces.
//
// Takes columns ([]sqlmodel.Column) which are the column definitions.
//
// Returns CatalogueMutationOption which sets Columns.
func WithColumns(columns []sqlmodel.Column) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.Columns = columns
	}
}

// WithEnum sets the enum type a mutation creates or alters, and its values.
//
// Takes enumName (string) which is the enum type's name.
// Takes enumValues ([]string) which are its values, in order.
//
// Returns CatalogueMutationOption which sets EnumName and EnumValues.
func WithEnum(enumName string, enumValues []string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.EnumName = enumName
		mutation.EnumValues = enumValues
	}
}

// WithFunction sets the function a mutation creates or drops.
//
// Takes definition (*FunctionDefinition) which is the function's definition.
//
// Returns CatalogueMutationOption which sets Function.
func WithFunction(definition *FunctionDefinition) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.Function = definition
	}
}

// WithTriggerName sets the trigger a mutation creates or drops.
//
// Takes triggerName (string) which is the trigger's name.
//
// Returns CatalogueMutationOption which sets TriggerName.
func WithTriggerName(triggerName string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.TriggerName = triggerName
	}
}

// WithPrimaryKey sets the primary key columns of a table a mutation creates.
//
// Takes primaryKey ([]string) which are the primary key column names, in order.
//
// Returns CatalogueMutationOption which sets PrimaryKey.
func WithPrimaryKey(primaryKey []string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.PrimaryKey = primaryKey
	}
}

// WithConstraints sets the table constraints a mutation creates or adds.
//
// Takes constraints ([]sqlmodel.Constraint) which are the table constraints.
//
// Returns CatalogueMutationOption which sets Constraints.
func WithConstraints(constraints []sqlmodel.Constraint) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.Constraints = constraints
	}
}

// WithConstraintName sets the constraint a mutation drops.
//
// Takes constraintName (string) which is the constraint's name.
//
// Returns CatalogueMutationOption which sets ConstraintName.
func WithConstraintName(constraintName string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.ConstraintName = constraintName
	}
}

// WithInheritsTables sets the parent tables a created table inherits columns from.
//
// Takes inheritsTables ([]TableReference) which are the parent tables, in order.
//
// Returns CatalogueMutationOption which sets InheritsTables.
func WithInheritsTables(inheritsTables []TableReference) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.InheritsTables = inheritsTables
	}
}

// WithPartitionOf sets the partitioned table a mutation's table is, or becomes or stops
// being, a partition of.
//
// Takes parent (TableReference) which names the partitioned table.
//
// Returns CatalogueMutationOption which sets PartitionOf.
func WithPartitionOf(parent TableReference) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.PartitionOf = &parent
	}
}

// WithWithoutRowID records whether a created table was declared WITHOUT ROWID.
//
// Takes isWithoutRowID (bool) which is true when the table has no implicit rowid.
//
// Returns CatalogueMutationOption which sets IsWithoutRowID.
func WithWithoutRowID(isWithoutRowID bool) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.IsWithoutRowID = isWithoutRowID
	}
}

// WithVirtualModule marks a created table as virtual, backed by the named module.
//
// Takes moduleName (string) which is the module from the USING clause.
//
// Returns CatalogueMutationOption which sets IsVirtual and VirtualModuleName.
func WithVirtualModule(moduleName string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.IsVirtual = true
		mutation.VirtualModuleName = moduleName
	}
}

// WithSequence sets the sequence a mutation creates or drops.
//
// Takes sequenceName (string) which is the sequence's name.
//
// Returns CatalogueMutationOption which sets SequenceName.
func WithSequence(sequenceName string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.SequenceName = sequenceName
	}
}

// WithSequenceOwner sets the table column that owns a sequence.
//
// Takes ownedByTable (string) which is the owning table's name.
// Takes ownedByColumn (string) which is the owning column's name.
//
// Returns CatalogueMutationOption which sets OwnedByTable and OwnedByColumn.
func WithSequenceOwner(ownedByTable string, ownedByColumn string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.OwnedByTable = ownedByTable
		mutation.OwnedByColumn = ownedByColumn
	}
}

// WithTypeName sets the user-defined type, such as an enum or composite type, a mutation
// creates, alters, or drops.
//
// Takes typeName (string) which is the type's name.
//
// Returns CatalogueMutationOption which sets EnumName.
func WithTypeName(typeName string) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.EnumName = typeName
	}
}

// WithColumnNullability makes an ALTER COLUMN mutation change only whether its column may
// hold NULL, keeping the column's type, default and every other attribute, as SET NOT
// NULL and DROP NOT NULL do.
//
// Takes nullable (bool) which is true for DROP NOT NULL and false for SET NOT NULL.
//
// Returns CatalogueMutationOption which sets DropsNotNull or SetsNotNull.
func WithColumnNullability(nullable bool) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.DropsNotNull = nullable
		mutation.SetsNotNull = !nullable
	}
}

// WithColumnType makes an ALTER COLUMN mutation change only its column's type, keeping
// its nullability, default and every other attribute, as ALTER COLUMN c TYPE t does.
//
// Takes sqlType (sqlmodel.SQLType) which is the new type.
// Takes arrayDimensions (int) which is the number of array dimensions the new type has.
//
// Returns CatalogueMutationOption which sets SetsType and Columns.
func WithColumnType(sqlType sqlmodel.SQLType, arrayDimensions int) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		column := sqlmodel.NewColumn(mutation.ColumnName, sqlType, true)
		column.ArrayDimensions = arrayDimensions
		column.IsArray = arrayDimensions > 0
		mutation.Columns = []sqlmodel.Column{column}
		mutation.SetsType = true
	}
}

// WithDomain sets the domain a CREATE DOMAIN mutation creates: its name and the base type
// it is declared over.
//
// Takes name (string) which is the domain's name.
// Takes baseType (sqlmodel.SQLType) which is the base type.
// Takes arrayDimensions (int) which is the number of array dimensions of the base type.
// Takes notNull (bool) which is true when the domain is declared NOT NULL.
//
// Returns CatalogueMutationOption which sets EnumName and Columns.
func WithDomain(name string, baseType sqlmodel.SQLType, arrayDimensions int, notNull bool) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		column := sqlmodel.NewColumn(name, baseType, !notNull)
		column.ArrayDimensions = arrayDimensions
		column.IsArray = arrayDimensions > 0
		mutation.EnumName = name
		mutation.Columns = []sqlmodel.Column{column}
	}
}

// WithTargetKind restricts a mutation to an object of one kind, such as a sequence.
//
// Takes kind (ObjectKind) which is the kind of object the mutation may change.
//
// Returns CatalogueMutationOption which sets TargetKind.
func WithTargetKind(kind ObjectKind) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.TargetKind = kind
	}
}

// WithUnanalysedPart records a part of the statement the dialect could not analyse. Each
// use adds one part after those already recorded.
//
// Takes part (Warning) which says what was skipped and where.
//
// Returns CatalogueMutationOption which appends to UnanalysedParts.
func WithUnanalysedPart(part Warning) CatalogueMutationOption {
	return func(mutation *CatalogueMutation) {
		mutation.UnanalysedParts = append(mutation.UnanalysedParts, part)
	}
}
