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

// GroupingSets describes a GROUP BY whose grouping modifier, such as ROLLUP, CUBE,
// GROUPING SETS or MySQL's WITH ROLLUP, groups different rows of the result by different
// sets of its keys: a subtotal row groups by fewer keys than a detail row, and a grand
// total row by none.
type GroupingSets struct {
	// Keys holds grouping expressions omitted by some result rows. Those expressions become
	// nullable unless FillsDefaults is set.
	Keys []Expression

	// FillsDefaults reports an engine that holds the default value of a key's type, rather
	// than NULL, in a row that does not group by the key, as ClickHouse does unless its
	// group_by_use_nulls setting is on.
	FillsDefaults bool
}

// RawQueryAnalysis is the dialect's initial analysis of a query before the analyser
// applies type resolution and nullability propagation.
type RawQueryAnalysis struct {
	// Detail holds additional query information defined by the dialect. The core analyser
	// does not interpret it.
	Detail EngineDetail

	// InsertTable is the target table name for INSERT statements.
	InsertTable string

	// InsertTarget is the target table of an INSERT statement, with its schema as the
	// statement writes it, so a code generator can name the table exactly as the query does.
	// It is zero for any other statement.
	InsertTarget TableReference

	// GroupingSets describes the query's grouping modifier, such as ROLLUP, CUBE, GROUPING
	// SETS or WITH ROLLUP, which groups different rows of the result by different sets of
	// keys. It is nil for a query without one.
	GroupingSets *GroupingSets

	// InsertSelect holds the analysis of an INSERT ... SELECT statement's SELECT body.
	//
	// It is populated when the inserted rows come from a query rather than a VALUES list.
	// The body carries its own FROM/JOIN relations and parameters; the analyser resolves
	// those against the body's own scope (not the INSERT target's), so a parameter or column
	// referencing the SELECT source resolves correctly. Nil for INSERT ... VALUES and
	// non-INSERT statements.
	InsertSelect *RawQueryAnalysis

	// CompoundBranches holds the branches of a compound query (UNION, UNION ALL, INTERSECT,
	// EXCEPT).
	CompoundBranches []RawCompoundBranch

	// RawTableValuedFunctions holds unresolved table-valued function calls in FROM clauses.
	// The analyser resolves each into a derived table in the query's scope.
	RawTableValuedFunctions []RawTableValuedFunctionReference

	// CTEDefinitions holds any WITH clause CTE definitions.
	CTEDefinitions []RawCTEDefinition

	// OutputColumns holds the unresolved output column references.
	OutputColumns []RawOutputColumn

	// GroupByColumns holds the columns referenced in a GROUP BY clause.
	GroupByColumns []ColumnReference

	// FromTables holds the tables referenced in the FROM clause.
	FromTables []TableReference

	// JoinClauses holds the JOIN clauses with their types.
	JoinClauses []JoinClause

	// ParameterReferences holds the unresolved parameter references.
	ParameterReferences []RawParameterReference

	// InsertColumns holds the target column names for INSERT statements.
	InsertColumns []string

	// ArrayJoinClauses holds ClickHouse-style `[LEFT] ARRAY JOIN` entries.
	//
	// Each clause unfolds an array source column into rows and exposes the element under the
	// supplied alias. The analyser resolves the element type by looking up the source on the
	// FROM tables and registering the alias in the scope chain.
	ArrayJoinClauses []RawArrayJoinClause

	// OrderByColumns captures the structured ORDER BY column list.
	//
	// Each entry records the textual expression plus the optional ASC/DESC direction, NULLS
	// FIRST/LAST nulls placement, and WITH FILL modifiers a ClickHouse query may attach.
	// Engines that surface only the textual form leave the slice nil; consumers should treat
	// nil as "no metadata available" rather than "no ORDER BY".
	OrderByColumns []OrderByColumn

	// RawDerivedTables holds unresolved subqueries in FROM clauses. The analyser resolves
	// each into a derived table in the query's scope.
	RawDerivedTables []RawDerivedTableReference

	// ClauseExpressions holds clause expressions outside the select list in source order.
	// Nested queries keep their expressions in their own analysis.
	ClauseExpressions []ClauseExpression

	// ArgumentNames maps numbered query parameters to the function-argument names they use.
	// It is nil when no named arguments are recorded.
	ArgumentNames map[int]string

	// FromOrder lists relation names or aliases in FROM order for star expansion. Nil uses
	// the order of the recorded relation groups instead.
	FromOrder []string

	// PredicateSubqueries holds unresolved subqueries that appear in a token-scanned
	// predicate position such as a WHERE, HAVING, or JOIN ... ON clause.
	//
	// An example is the scalar subquery in `WHERE x.id = (SELECT MAX(y.id) FROM y WHERE y.id
	// < ?2)`. Such subqueries are not reached through the SELECT-list expression tree or the
	// FROM-clause derived tables, so the engine records each one here. The analyser resolves
	// their parameters and columns in a scope built from the subquery's own FROM/JOIN tables
	// and chained to the parent, so a subquery-local alias resolves locally and a correlated
	// reference resolves through the parent chain. Engines that leave this nil fall back to
	// the flat pass for the predicate subquery's parameters.
	PredicateSubqueries []*RawQueryAnalysis

	// Warnings holds dialect warnings in source order for the analyser to report at their
	// recorded locations. Dialects record them only on the top-level analysis.
	Warnings []Warning

	// Spans locates the parts of the statement a code generator cuts or rewrites, as byte
	// offsets from the statement's first byte. A dialect records them on the top-level
	// analysis only.
	Spans StatementSpans

	// ReturningLocation is the byte offset of the RETURNING keyword in the source SQL, in
	// the same terms as ParsedStatement.Location, when HasReturning is set. Zero means the
	// dialect did not record it, and a diagnostic about the clause points at the query
	// instead.
	ReturningLocation int

	// DataChange is the kind of the statement when it changes rows, such as DataChangeUpdate
	// for an UPDATE, and zero for a statement that only reads. A dialect sets it for every
	// statement it parses a RETURNING clause on, so the analyser can judge the clause
	// against the statements Info.Returning lists.
	DataChange DataChange

	// ConflictAction is what an INSERT does with a row that conflicts with an existing one,
	// as its ON CONFLICT clause or the dialect's equivalent says. It is ConflictActionNone
	// for a statement without such a clause, and the zero value, ConflictActionUnrecorded,
	// when the dialect does not record it.
	ConflictAction ConflictAction

	// AsyncMutation marks a statement whose changes finish after execution returns.
	// Supporting dialects recommend the asyncexec command for it.
	AsyncMutation bool

	// HasReturning indicates whether the statement has a RETURNING clause. A dialect sets it
	// whenever it parses the clause, including when Info reports no RETURNING support, so
	// the analyser can report the clause rather than generate a method the database rejects.
	HasReturning bool

	// ReadOnly indicates the query does not modify data. SELECT and VALUES statements are
	// read-only unless they contain FOR UPDATE/SHARE locking clauses or data-modifying CTEs
	// (INSERT/UPDATE/DELETE inside WITH).
	ReadOnly bool

	// HasWhereClause reports a WHERE clause on the statement, or a PREWHERE clause for
	// ClickHouse. Spans.Where locates an actual WHERE clause for SQL rewriting.
	HasWhereClause bool
}

// OrderDirection identifies the direction of an ORDER BY column.
type OrderDirection uint8

const (
	// OrderDirectionUnspecified indicates the column carried no explicit ASC / DESC keyword.
	// The engine's default ordering applies.
	OrderDirectionUnspecified OrderDirection = iota

	// OrderDirectionAsc indicates the column carried the ASC keyword.
	OrderDirectionAsc

	// OrderDirectionDesc indicates the column carried the DESC keyword.
	OrderDirectionDesc
)

// OrderNullsPlacement identifies the placement of NULL values in an ORDER BY column.
type OrderNullsPlacement uint8

const (
	// OrderNullsUnspecified indicates the column carried no explicit NULLS FIRST / NULLS
	// LAST clause.
	OrderNullsUnspecified OrderNullsPlacement = iota

	// OrderNullsFirst indicates the column carried NULLS FIRST.
	OrderNullsFirst

	// OrderNullsLast indicates the column carried NULLS LAST.
	OrderNullsLast
)

// ConflictAction identifies what an INSERT does with a row that conflicts with an
// existing one.
type ConflictAction uint8

const (
	// ConflictActionUnrecorded is the zero value: the dialect did not record whether the
	// statement handles conflicts, so nothing may be assumed either way.
	ConflictActionUnrecorded ConflictAction = iota

	// ConflictActionNone marks a statement without a conflict clause.
	ConflictActionNone

	// ConflictActionDoNothing marks an INSERT that skips a conflicting row, as ON CONFLICT
	// DO NOTHING does, so a RETURNING clause returns no row for it.
	ConflictActionDoNothing

	// ConflictActionDoUpdate marks an INSERT that updates the existing row instead, as ON
	// CONFLICT DO UPDATE does.
	ConflictActionDoUpdate
)

const (
	// CompoundUnion is the UNION operator (removes duplicates).
	CompoundUnion CompoundOperator = iota + 1

	// CompoundUnionAll is the UNION ALL operator (keeps duplicates).
	CompoundUnionAll

	// CompoundIntersect is the INTERSECT operator.
	CompoundIntersect

	// CompoundExcept is the EXCEPT operator.
	CompoundExcept
)

const (
	// JoinColumnsLead puts the merged columns first, in the order the USING clause lists
	// them or, for a NATURAL join, the order the left side has them, followed by the other
	// columns of the left side and then of the right side, as standard SQL does.
	JoinColumnsLead JoinColumnPlacement = iota

	// JoinColumnsInPlace keeps each merged column where the left side has it and drops it
	// from the right side.
	JoinColumnsInPlace
)

// String names the conflict action, such as "DO NOTHING", or gives the number of a value
// that is not a declared action.
//
// Returns string which is the name.
func (action ConflictAction) String() string {
	switch action {
	case ConflictActionUnrecorded:
		return "unrecorded"
	case ConflictActionNone:
		return "none"
	case ConflictActionDoNothing:
		return "DO NOTHING"
	case ConflictActionDoUpdate:
		return "DO UPDATE"
	default:
		return fmt.Sprintf("ConflictAction(%d)", uint8(action))
	}
}

// OrderByColumn captures one column of an ORDER BY clause with its direction, nulls
// placement, and optional WITH FILL modifiers.
//
// A dialect can reconstruct the expression text from tokens, so it need not preserve the
// source's whitespace or identifier quoting. Use StatementSpans.OrderBy and the original
// SQL when exact source text is needed.
type OrderByColumn struct {
	// Expression is the expression text captured by the dialect, with surrounding whitespace
	// stripped. It need not preserve source whitespace or identifier quoting.
	Expression string

	// FillFrom is the textual `WITH FILL FROM <expr>` value when present. Empty when the
	// column either lacks WITH FILL or carries it without a FROM bound.
	FillFrom string

	// FillTo is the textual `WITH FILL TO <expr>` value when present.
	FillTo string

	// FillStep is the textual `WITH FILL STEP <expr>` value when present.
	FillStep string

	// Direction captures the ASC / DESC modifier when present.
	Direction OrderDirection

	// Nulls captures the NULLS FIRST / NULLS LAST modifier when present.
	Nulls OrderNullsPlacement

	// HasFill indicates whether the column carried a WITH FILL modifier. Engines set this to
	// true even when no FROM / TO / STEP body was supplied so callers can distinguish "no
	// fill" from "fill with engine-default boundaries".
	HasFill bool
}

// RawArrayJoinClause is an unresolved ClickHouse ARRAY JOIN entry.
//
// Each entry unfolds a source whose value is an array into one row per element, and names
// the element by its alias. An entry whose alias is the name of the column it unfolds, as
// an entry without an alias is, unfolds the column in place: within the query the column
// names its element, a star lists the element where it lists the column, and the columns
// ElementPrefix extends the entry to unfold with it. An entry with another alias leaves
// its source as it is and adds the alias, which names the element without being part of
// the row a star lists.
//
// ClickHouse accepts both bare column references and arbitrary array-producing
// expressions as the source. SourceColumn carries the bare-column form; SourceExpression
// carries the textual body for the expression form such as literal arrays or
// arrayMap(...) results; Source carries either as an expression the analyser types.
type RawArrayJoinClause struct {
	// Source is the source as an expression for the analyser to type: a column reference for
	// a bare column, qualified when the query qualifies it, or the expression of an
	// expression source. It is nil when the dialect did not parse the source, and then
	// SourceColumn names the column.
	Source Expression

	// Alias is the name the unfolded element is exposed under. Required: the projection
	// references the alias by name.
	Alias string

	// SourceColumn is the bare column name (without any table qualification) whose array
	// element the alias represents. Populated only for the bare-column form; empty when the
	// source is an arbitrary expression.
	SourceColumn string

	// SourceExpression captures the textual body of an expression source such as `[1, 2, 3]`
	// or `arrayMap(f, src)` for the non-bare-column form, and is empty when the source is a
	// bare column.
	SourceExpression string

	// ElementPrefix includes related array columns when a nested structure unfolds in place.
	// Empty means only this entry unfolds.
	ElementPrefix string

	// IsLeft indicates the `LEFT ARRAY JOIN` variant, which keeps a row whose array is
	// empty, giving its element the default value of the element's type. The element is
	// therefore no more nullable than the array's element type says.
	IsLeft bool
}

// CompoundOperator identifies a compound query operator.
type CompoundOperator uint8

// Warning is something a dialect warns of in a statement it analyses: SQL the engine
// runs, but reads other than the statement appears to mean.
type Warning struct {
	// Message says how the engine reads the SQL and how to write it instead.
	Message string

	// Span locates the SQL the warning is about, as byte offsets into the SQL the dialect
	// parsed, the terms ParsedStatement.Location is in. Zero places the warning on the
	// statement.
	Span Span
}

// RawCompoundBranch holds a single branch of a compound query with its operator and
// unresolved query analysis.
type RawCompoundBranch struct {
	// Query is the unresolved analysis of this branch's SELECT.
	Query *RawQueryAnalysis

	// Operator is the compound operator preceding this branch.
	Operator CompoundOperator
}

// RawDerivedTableReference holds an unresolved subquery in a FROM clause. The analyser
// resolves the inner query's output columns and registers them as a derived table in the
// query's scope.
type RawDerivedTableReference struct {
	// InnerQuery is the unresolved analysis of the subquery.
	InnerQuery *RawQueryAnalysis

	// Using describes the columns the join merges when the derived table is joined with a
	// USING clause or a NATURAL join, and is nil otherwise.
	Using *JoinUsing

	// Alias is the required alias for the derived table.
	Alias string

	// ColumnNames holds column aliases that rename the inner query's columns in order. A
	// shorter list renames only the leading columns; an empty list preserves every name.
	ColumnNames []string

	// JoinKind indicates how this derived table is joined.
	JoinKind JoinKind
}

// TVFColumnDefinition holds a column name and optional type name from an AS alias(col1
// type1, col2 type2) clause on a table-valued function.
type TVFColumnDefinition struct {
	// Name is the column name.
	Name string

	// TypeName is the raw engine type name (e.g. "text", "integer", "int4[]"), or empty when
	// the column definition provides only a name without a type.
	TypeName string
}

// RawTableValuedFunctionReference holds an unresolved table-valued function call in a
// FROM clause (e.g. json_each, generate_series).
type RawTableValuedFunctionReference struct {
	// FunctionName is the table-valued function name.
	FunctionName string

	// Arguments holds the arguments of the call, in order, for a dialect whose table-valued
	// functions give columns typed by their arguments, as a
	// dialect.TableValuedFunctionTyperPort types them. It may be nil for a dialect that has
	// none, and is nil for a reference of several calls, whose own arguments Functions
	// holds.
	Arguments []Expression

	// Alias is the table alias used in the query.
	Alias string

	// ColumnDefinitions holds the column definitions from an AS alias(name type, ...)
	// clause. Empty when no column definitions are provided.
	ColumnDefinitions []TVFColumnDefinition

	// Ordinality describes the row-number column added by WITH ORDINALITY. It follows the
	// function's columns and is nil when ordinality is absent.
	Ordinality *ScopedColumn

	// Functions holds the ordered calls supplying columns for a combined table reference,
	// such as ROWS FROM. It is nil for a single function call.
	Functions []TableFunctionCall

	// JoinKind indicates how this table-valued function is joined.
	JoinKind JoinKind
}

// TableFunctionCall is one of the function calls whose columns a table reference reads
// together, as in ROWS FROM (f(...), g(...)).
type TableFunctionCall struct {
	// FunctionName is the function's name as the reference writes it, lower-cased and
	// optionally schema-qualified.
	FunctionName string

	// Arguments holds the call's arguments, in order, as RawTableValuedFunctionReference
	// describes; an unnest of several arrays read as one unnest per array holds that array
	// alone.
	Arguments []Expression

	// ColumnDefinitions holds the call's own column definition list, AS (name type, ...),
	// which gives the columns of a function returning a record, or is empty.
	ColumnDefinitions []TVFColumnDefinition
}

// RawOutputColumn is an unresolved output column from the dialect.
type RawOutputColumn struct {
	// Expression holds the typed expression for computed columns.
	Expression Expression

	// Name is the column alias or inferred name.
	Name string

	// TableAlias is the table alias or name this column references, if any.
	TableAlias string

	// ColumnName is the referenced column name, if this is a direct reference.
	ColumnName string

	// IsStar indicates this is a SELECT * expansion.
	IsStar bool
}

// RawParameterReference is an unresolved parameter reference from the engine.
type RawParameterReference struct {
	// ColumnReference is the column this parameter is compared with or assigned to, if
	// applicable.
	ColumnReference *ColumnReference

	// CastType supplies the parameter type from an explicit cast or a compared function
	// result. It takes precedence during type inference.
	CastType *sqlmodel.SQLType

	// Name is the identifier for named parameters (:email, @user_id, $name). Empty for
	// positional/numbered parameters.
	Name string

	// EnclosingFunctionName is the name of the function or table-valued function whose
	// argument list this parameter sits in.
	//
	// It is recorded as the engine writes it (lower-cased, optionally schema-qualified, such
	// as "content.get_pages_with_latest_version" or "string_to_array") and is empty when the
	// parameter is not a function argument. The analyser pairs it with ArgumentOrdinal to
	// look up the matched function signature's declared argument type and back-propagate it
	// onto an otherwise untyped placeholder.
	EnclosingFunctionName string

	// Number is the positional parameter number ($1, ?1, etc.) or sequential ordinal for
	// anonymous question-mark style.
	Number int

	// ArgumentOrdinal is the zero-based position of this parameter among the enclosing
	// call's top-level arguments (the call-site argument slot, NOT the parameter number).
	// Only meaningful when Context is ParameterContextFunctionArgument and
	// EnclosingFunctionName is non-empty.
	ArgumentOrdinal int

	// Context describes where the parameter appears (comparison, function arg, assignment,
	// cast, etc.) for type inference.
	Context sqlmodel.ParameterContext
}

// ColumnReference identifies a specific column in a specific table.
type ColumnReference struct {
	// TableAlias is the table alias or name.
	TableAlias string

	// ColumnName is the column name.
	ColumnName string
}

// TableReference identifies a table in the FROM clause.
type TableReference struct {
	// Schema is the table's schema.
	Schema string

	// Name is the table name.
	Name string

	// Alias is the table alias, if specified.
	Alias string
}

// JoinClause describes a JOIN in the query.
type JoinClause struct {
	// Using describes the columns the join merges when it has a USING clause or is a NATURAL
	// join, and is nil otherwise.
	Using *JoinUsing

	// Table is the joined table.
	Table TableReference

	// Kind identifies the join type.
	Kind JoinKind
}

// JoinColumnPlacement says where a column that a USING clause or a NATURAL join merges
// appears in the joined row an unqualified star expands to.
type JoinColumnPlacement uint8

// JoinUsing describes the columns a join with a USING clause, or a NATURAL join, merges:
// each appears once in the joined row and can be named without a qualifier.
type JoinUsing struct {
	// Columns lists the columns the USING clause names, in order. It is empty for a NATURAL
	// join, which merges every column name both sides have.
	Columns []string

	// Natural reports a NATURAL join.
	Natural bool

	// Placement says where the dialect puts the merged columns in the joined row.
	Placement JoinColumnPlacement
}

// RawCTEDefinition is an unresolved CTE from the dialect.
type RawCTEDefinition struct {
	// Detail holds additional CTE information defined by the dialect. The core analyser does
	// not interpret it.
	Detail EngineDetail

	// Name is the CTE name.
	Name string

	// OutputColumns holds the CTE body's output columns, if resolvable, under the names the
	// body gives them; ColumnNames renames them.
	OutputColumns []RawOutputColumn

	// ColumnNames holds aliases that rename the CTE body's columns in order after star
	// expansion. A shorter list renames only the leading columns; an empty list preserves
	// every name.
	ColumnNames []string

	// FromTables holds the tables referenced in the CTE body's FROM clause.
	FromTables []TableReference

	// JoinClauses holds JOIN clauses from the CTE body.
	JoinClauses []JoinClause

	// CompoundBranches holds UNION/INTERSECT/EXCEPT branches from the CTE body.
	CompoundBranches []RawCompoundBranch

	// FromOrder lists visible relation names in the CTE body's FROM order. Nil means the
	// dialect did not record it.
	FromOrder []string

	// Query holds the CTE body's analysis, or nil when the dialect records none. When
	// present, it lets the analyser resolve the body's nested queries and check its column
	// and function references.
	Query *RawQueryAnalysis

	// ParameterReferences holds the parameters that occur in the CTE body.
	//
	// Engines that flatten a CTE body's parameters into the top-level
	// RawQueryAnalysis.ParameterReferences (for parameter-count purposes) also record them
	// here so the shared analyser can resolve each one against the CTE body's own FROM/JOIN
	// scope rather than the outer query scope. A CTE-body column reference (for example an
	// unqualified id) is frequently ambiguous in the outer scope yet unambiguous inside the
	// CTE body; resolving in the body scope removes that false Q002/Q001. Engines that
	// number CTE-body parameters independently (and so do not flatten them) may leave this
	// empty; the analyser's identity guard makes population safe either way.
	ParameterReferences []RawParameterReference

	// IsRecursive indicates whether this is a recursive CTE.
	IsRecursive bool
}

// ScopedColumn is a column within a scoped table, carrying its resolved type and
// JOIN-adjusted nullability.
type ScopedColumn struct {
	// Name is the column name.
	Name string

	// SQLType is the resolved SQL type from the catalogue.
	SQLType sqlmodel.SQLType

	// Nullable indicates whether this column can be NULL in the current scope, accounting
	// for JOIN type adjustments.
	Nullable bool
}

// EngineDetail is what a dialect records about a statement beyond the neutral analysis,
// such as the options of a clause only its engine has. The analyser never reads it, so a
// dialect defines its own type, and a new engine's clauses need no change here.
type EngineDetail interface {
	// EngineDetail marks the type as a dialect's detail.
	EngineDetail()
}
