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

package diag

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// codePrefix is the letter every diagnostic code starts with.
const codePrefix = "Q"

// CodeInfo documents one diagnostic code: its title, the severity it is normally reported
// with and a Markdown explanation of its cause and fix.
type CodeInfo struct {
	// Code is the stable diagnostic code, such as "Q012".
	Code string

	// Title is a short sentence-case summary without a trailing full stop.
	Title string

	// Explanation is a Markdown body describing what triggers the diagnostic, why it matters
	// and how to fix it. Paragraphs are separated by a blank line.
	Explanation string

	// Severity is the severity the generator normally reports the code with.
	Severity ErrorSeverity
}

// codeTable documents every diagnostic code, in ascending order.
var codeTable = []CodeInfo{
	{
		Code:     CodeUnknownColumn,
		Title:    "Unknown column",
		Severity: SeverityWarning,
		Explanation: "A column reference does not match any column of the tables, views, aliases or common table " +
			"expressions in scope. The message names the column and, where one was given, the table or alias it " +
			"was looked up in.\n\n" +
			"Seagull cannot infer the type of a column it cannot find, so the generated field or parameter cannot " +
			"be typed precisely. The diagnostic is a warning because the database may hold objects the " +
			"migrations do not describe, but it is almost always a typo or a missing migration.\n\n" +
			"Check the spelling, check that a migration creates the column, and qualify the reference with the " +
			"right table alias:\n\n" +
			"```sql\n" +
			"-- sgl.query(GetUser, one)\n" +
			"SELECT u.id, u.email FROM users u WHERE u.id = $1;\n" +
			"```",
	},
	{
		Code:     CodeAmbiguousColumn,
		Title:    "Ambiguous column reference",
		Severity: SeverityWarning,
		Explanation: "An unqualified column name matches columns in more than one table in scope, typically in a " +
			"join where both sides have an `id` or `created_at` column.\n\n" +
			"The database would reject the query, and Seagull cannot tell which column's type to use. Qualify the " +
			"column with the table name or alias:\n\n" +
			"```sql\n" +
			"SELECT o.id, c.name FROM orders o JOIN customers c ON c.id = o.customer_id;\n" +
			"```",
	},
	{
		Code:     CodeUnknownTable,
		Title:    "Unknown table",
		Severity: SeverityWarning,
		Explanation: "A table, view, common table expression or table-valued function named in the query is not " +
			"in the schema catalogue built from the migrations.\n\n" +
			"Without the table Seagull cannot type the columns read from it. Check the spelling and the schema " +
			"qualification, and check that a migration creates the table. A table-valued function that comes " +
			"from a database extension can be declared as a custom function in the configuration.",
	},
	{
		Code:     CodeExpressionTypeError,
		Title:    "Expression type cannot be inferred",
		Severity: SeverityWarning,
		Explanation: "Seagull could not work out the SQL type of an expression in the select list or of a " +
			"parameter, usually because an operator or function has operands of incompatible or unknown types. A " +
			"parameter a statement uses where two types are expected that the engine converts between only " +
			"explicitly, as in `UPDATE authors SET name = $1 WHERE id = $1`, is reported too: the generated code " +
			"takes one of the types, and a value of the other fails.\n\n" +
			"The generated Go type cannot be precise. Add an explicit `CAST`, or pin the output column's " +
			"type with a `sgl.column` directive:\n\n" +
			"```sql\n" +
			"-- sgl.query(GetTotal, one)\n" +
			"-- sgl.column(total, type: numeric)\n" +
			"SELECT SUM(amount) AS total FROM payments;\n" +
			"```",
	},
	{
		Code:     CodeUnknownFunction,
		Title:    "Unknown function",
		Severity: SeverityWarning,
		Explanation: "A function call names a function the dialect does not know and the migrations do not " +
			"create, or no overload of the function accepts the given number and types of arguments.\n\n" +
			"Seagull cannot infer the type of the call's result. Check the spelling and the arguments. A function " +
			"from a database extension can be declared under `custom_functions` in the configuration (or with " +
			"`WithCustomFunctions` when embedding the library), giving its argument types, return type and " +
			"null behaviour.\n\n" +
			"An unknown function may change data, so `NewWithReplica` sends a query that calls one to the writer. " +
			"A declared custom function is read-only unless it sets `data_access: modifies_data`, so declaring it " +
			"also sends the query back to the reader; `readonly: true` on the query header overrides the routing " +
			"without typing the call.",
	},
	{
		Code:     CodeDuplicateQueryName,
		Title:    "Duplicate query name",
		Severity: SeverityError,
		Explanation: "Two queries declare the same name in their `sgl.query` headers. Each name becomes a Go " +
			"method and the names of its types, so a duplicate would not compile. Names are compared regardless of " +
			"letter case and of the underscores between words, since a snake_case name becomes the method of its " +
			"PascalCase form: `get_user` and `GetUser` are both `GetUser`.\n\n" +
			"Rename one of the queries. Names must be unique across every query file of a database.",
	},
	{
		Code:     CodeDirectiveSyntax,
		Title:    "Malformed directive",
		Severity: SeverityError,
		Explanation: "A directive comment could not be parsed: an argument is out of place, a separator is " +
			"missing, or the call is otherwise not of the form `sgl.<name>(<positionals>, <key>: <value>)`.\n\n" +
			"Fix the directive using the form shown in the directive reference. `sgl fmt` can show the canonical " +
			"layout of directives that do parse.",
	},
	{
		Code:     CodeMissingDirective,
		Title:    "Query header is missing its name or command",
		Severity: SeverityError,
		Explanation: "A `sgl.query` header does not give the query's name or its command. Both are required: the " +
			"name becomes the Go method and the command decides its shape.\n\n" +
			"Give both, positionally or by keyword:\n\n" +
			"```sql\n" +
			"-- sgl.query(ListTasks, many)\n" +
			"SELECT id, title FROM tasks;\n" +
			"```",
	},
	{
		Code:     CodeCommandOutputMismatch,
		Title:    "Command does not match the query's output",
		Severity: SeverityWarning,
		Explanation: "The declared command does not fit what the statement returns: `one`, `many` or `stream` on a " +
			"statement that returns no columns, or `exec`, `execrows`, `execresult` or `asyncexec` on a statement " +
			"that does. A `batch` or `copyfrom` method returns only an error too, so a `SELECT` or a `RETURNING` " +
			"clause in one has its rows discarded.\n\n" +
			"The generated method would either have nothing to scan or would discard the rows. Pick the command " +
			"that matches the statement: add a `RETURNING` clause to an `INSERT`, `UPDATE` or `DELETE` that should " +
			"return rows, or use an exec command when no rows are wanted. A statement whose only columns are the " +
			"results of functions that return no value, such as PostgreSQL's `SELECT pg_advisory_xact_lock($1)`, " +
			"returns nothing to read, so it fits an exec command without this warning.",
	},
	{
		Code:     CodeParseError,
		Title:    "SQL cannot be parsed or analysed",
		Severity: SeverityError,
		Explanation: "A migration or query holds SQL the dialect's parser rejects, a query block holds no " +
			"statement at all, or a statement parsed but could not be analysed or applied to the schema " +
			"catalogue.\n\n" +
			"In a migration it stops generation, because the schema is unknown. In a query, `sgl generate` " +
			"leaves that query out and writes the rest of the package, and still exits with status 1, because " +
			"the query's shape is unknown. Correct the SQL. If the statement is valid for your database but " +
			"Seagull does not understand it, please report it with the statement and the dialect.",
	},
	{
		Code:     CodeSortableColumnMissing,
		Title:    "Sortable column is not in the query output",
		Severity: SeverityWarning,
		Explanation: "A `sgl.sortable` directive lists a column that the query does not select. The generated " +
			"builder only orders by columns in its allow-list, and ordering by a column the result does not carry " +
			"is not what the list intends.\n\n" +
			"Add the column to the select list, or remove it from the sortable list:\n\n" +
			"```sql\n" +
			"-- sgl.sortable(order_by, [name, price, created_at])\n" +
			"```",
	},
	{
		Code:     CodeMultipleStatements,
		Title:    "Query block holds more than one statement",
		Severity: SeverityError,
		Explanation: "A query block contains several SQL statements. The generated method would send them all, " +
			"but only the last one is analysed, so its result type, its parameters and whether it may run on a " +
			"read replica would describe a different statement from the one that writes. A misspelt query header " +
			"between two queries used to cause this.\n\n" +
			"Give each statement its own `sgl.query` header so each becomes its own method.",
	},
	{
		Code:     CodeGeneratedColumn,
		Title:    "Parameter writes to a generated column",
		Severity: SeverityWarning,
		Explanation: "A parameter is assigned to a generated (computed) column in an `INSERT` or `UPDATE`. The " +
			"database computes such columns itself and rejects a statement that writes to them.\n\n" +
			"Remove the column from the statement's column list or `SET` clause.",
	},
	{
		Code:     CodeGroupByColumnMissing,
		Title:    "Grouping column is not in the query output",
		Severity: SeverityWarning,
		Explanation: "The `group_by` argument of a `sgl.query` header names a column the query does not select. " +
			"The generated code collapses repeated parent rows by that column, so it must be in the result.\n\n" +
			"Select the column, or correct the `group_by` reference.",
	},
	{
		Code:     CodeGroupByMissingEmbed,
		Title:    "Grouping has no embedded child rows",
		Severity: SeverityWarning,
		Explanation: "A query declares `group_by` but no `sgl.embed` directive groups the columns of the other " +
			"tables in the join. Grouping exists to collect child rows under each parent row, so without an embed " +
			"there is nothing to collect.\n\n" +
			"Add a `sgl.embed` directive for the child table, or drop `group_by`:\n\n" +
			"```sql\n" +
			"-- sgl.query(ListAuthorsWithBooks, many, group_by: authors.id)\n" +
			"-- sgl.embed(books, from: b)\n" +
			"SELECT authors.id, authors.name, b.id, b.title\n" +
			"FROM authors JOIN books b ON b.author_id = authors.id;\n" +
			"```",
	},
	{
		Code:     CodeGroupByWrongCommand,
		Title:    "Grouping used with a command other than many",
		Severity: SeverityWarning,
		Explanation: "`group_by` collapses many joined rows into fewer parent rows, which only makes sense for a " +
			"query that returns a list.\n\n" +
			"Use the `many` command, or remove `group_by`.",
	},
	{
		Code:     CodeSliceBatchCopyFrom,
		Title:    "Slice parameter used with batch or copyfrom",
		Severity: SeverityError,
		Explanation: "A parameter declared with `kind: slice` appears in a `batch` or `copyfrom` query. Those " +
			"commands send one row of parameters per item and cannot expand a slice into an `IN` list.\n\n" +
			"Use a plain parameter, or move the slice parameter to a `one`, `many` or exec query.",
	},
	{
		Code:     CodeSliceDynamicRuntime,
		Title:    "Slice parameter used with a runtime builder",
		Severity: SeverityError,
		Explanation: "A parameter declared with `kind: slice` appears in a query with `dynamic: runtime`. The " +
			"runtime builder assembles its `WHERE` clause at call time and cannot also expand slice " +
			"placeholders.\n\n" +
			"Remove `dynamic: runtime`, or replace the slice parameter with a builder predicate.",
	},
	{
		Code:     CodeCompoundColumnCount,
		Title:    "Column counts do not match",
		Severity: SeverityError,
		Explanation: "A branch of a `UNION`, `INTERSECT` or `EXCEPT` selects a different number of columns from " +
			"the first branch. The database rejects such a query, so this is an error.\n\n" +
			"The same code is reported as a warning when a table-valued function is given more column aliases " +
			"than it has columns; the surplus aliases are ignored.\n\n" +
			"Make every branch select the same number of columns, or remove the surplus aliases.",
	},
	{
		Code:     CodeRuntimeBuilderBaseHasWhere,
		Title:    "Runtime builder extends an existing WHERE clause",
		Severity: SeverityHint,
		Explanation: "A `dynamic: runtime` query already has a `WHERE` clause, so predicates added through the " +
			"builder at call time are joined to it with `AND` rather than starting a new `WHERE`.\n\n" +
			"This is informational. Check that the fixed conditions should always apply alongside the dynamic ones.",
	},
	{
		Code:     CodeCountSemanticsWrapped,
		Title:    "Count query wraps the original query",
		Severity: SeverityHint,
		Explanation: "The count query generated for a `dynamic: runtime` query wraps the original statement as " +
			"`SELECT COUNT(*) FROM (...) sub`, because the rows the statement returns are not the rows it reads: " +
			"it uses `GROUP BY`, `HAVING`, `DISTINCT`, an aggregate or a window function in its select list, a " +
			"clause that shapes its rows after `HAVING`, or it is compound. The count therefore reports the " +
			"number of result rows, not the number of rows in the underlying table.\n\n" +
			"This is informational; no change is needed.",
	},
	{
		Code:     CodeCountRewriteUnavailable,
		Title:    "Count query cannot be generated",
		Severity: SeverityWarning,
		Explanation: "A `dynamic: runtime` query could not be rewritten into a count query, because its top level " +
			"is not a `SELECT` or has no `FROM` to count from. The builder is still generated, but without its " +
			"count method.\n\n" +
			"Simplify the statement's top level if you need the count, or ignore the warning if you do not.",
	},
	{
		Code:     CodeUnknownDirective,
		Title:    "Unknown directive",
		Severity: SeverityError,
		Explanation: "A comment uses the directive prefix with a name that is not a known directive, such as " +
			"`sgl.qurey`. The message suggests the closest known directive when there is one.\n\n" +
			"Correct the directive name. The directive reference lists every directive.",
	},
	{
		Code:     CodeUnknownKeywordArgument,
		Title:    "Unknown keyword argument",
		Severity: SeverityError,
		Explanation: "A directive is given a keyword argument it does not accept, such as `nullabel:` on " +
			"`sgl.param`. The message suggests the closest accepted keyword when there is one.\n\n" +
			"Correct or remove the keyword argument.",
	},
	{
		Code:     CodeInvalidKeywordArgumentValue,
		Title:    "Invalid keyword argument value",
		Severity: SeverityError,
		Explanation: "A keyword argument has a value of the wrong shape: a word outside its allowed set, text " +
			"where a number or boolean is expected, or a scalar where a list is expected. The message lists the " +
			"allowed values or the expected shape, and suggests the closest allowed value when there is one.\n\n" +
			"The same code is reported when a value names something the query does not hold: a `from:` alias on " +
			"`sgl.embed` that is not in the `FROM` clause, belongs to another table or is already embedded, or an " +
			"`as:` name another embed of the query already uses.\n\n" +
			"Correct the value.",
	},
	{
		Code:     CodeDuplicateKeywordArgument,
		Title:    "Keyword argument given twice",
		Severity: SeverityError,
		Explanation: "The same keyword argument appears more than once in one directive call, so its value is " +
			"ambiguous.\n\n" +
			"Remove one of the two.",
	},
	{
		Code:     CodeInternalNilGuard,
		Title:    "Internal analysis guard",
		Severity: SeverityWarning,
		Explanation: "Analysis met an internal inconsistency, such as a missing column, derived table or " +
			"compound branch, and skipped the affected part of the query rather than failing. This should not " +
			"normally happen; please report it with the query and the dialect.\n\n" +
			"The same code is used when a custom function declared in the configuration cannot be converted, for " +
			"example because its return type is not a type the dialect knows. Check the declaration in that case.",
	},
	{
		Code:     CodeMissingRequired,
		Title:    "Required directive argument missing",
		Severity: SeverityError,
		Explanation: "A directive call lacks a required positional or keyword argument, such as the table name " +
			"of `sgl.embed`.\n\n" +
			"Some arguments are required only in context. `sgl.embed` needs `from:` when its table appears more " +
			"than once in the `FROM` clause, as in a self-join, and `as:` when another embed of the query already " +
			"has its name:\n\n" +
			"```sql\n" +
			"-- sgl.embed(employees, from: m, as: Manager)\n" +
			"```\n\n" +
			"Add the argument named in the message.",
	},
	{
		Code:     CodeUnclosedDirective,
		Title:    "Directive call is not closed",
		Severity: SeverityError,
		Explanation: "A directive call opens a parenthesis or bracket that is never closed before the directive " +
			"comments end. A call may span several comment lines, but each line must start with the comment " +
			"marker and the call must close:\n\n" +
			"```sql\n" +
			"-- sgl.query(\n" +
			"--   SearchOrders,\n" +
			"--   many,\n" +
			"-- )\n" +
			"```",
	},
	{
		Code:     CodeParameterBindingSyntax,
		Title:    "Malformed parameter binding",
		Severity: SeverityError,
		Explanation: "A parameter binding line is not of the form `$N as sgl.<role>(...)` (or `? as` for dialects " +
			"with positional placeholders): the placeholder, the `as` keyword, the directive prefix or the role is " +
			"missing or misspelt. The message suggests a fix for a near-miss spelling.\n\n" +
			"```sql\n" +
			"-- $1 as sgl.param(user_id)\n" +
			"```",
	},
	{
		Code:     CodeInvalidListLiteral,
		Title:    "List literal is not closed",
		Severity: SeverityError,
		Explanation: "A bracketed list value, such as the column list of `sgl.sortable`, ends with something other " +
			"than `]`, commonly a stray `)` or a missing `]`. The value is discarded.\n\n" +
			"Close the list with `]`.",
	},
	{
		Code:     CodeUnterminatedString,
		Title:    "Unterminated string in a directive",
		Severity: SeverityError,
		Explanation: "A quoted string in a directive, such as a `go_type:` value, has no closing quote before the " +
			"end of the line or file.\n\n" +
			"Add the closing quote.",
	},
	{
		Code:     CodeUnknownOverrideColumn,
		Title:    "Column override names an unknown output column",
		Severity: SeverityError,
		Explanation: "A `sgl.column` directive in a query header names a column the query does not select. The " +
			"message suggests the closest output column.\n\n" +
			"Use the column's output name, which is its alias when it has one:\n\n" +
			"```sql\n" +
			"-- sgl.query(GetUserEmailLower, one)\n" +
			"-- sgl.column(email_lower, type: text)\n" +
			"SELECT id, LOWER(email) AS email_lower FROM users WHERE id = $1;\n" +
			"```",
	},
	{
		Code:     CodeUnknownOverrideMigrationColumn,
		Title:    "Column override names an unknown table column",
		Severity: SeverityError,
		Explanation: "A `sgl.column(table.column, ...)` directive in a migration names a column that does not " +
			"exist on that table in the schema built so far. The message suggests the closest known " +
			"`table.column`.\n\n" +
			"Correct the reference, and place the directive in or after the migration that creates the column. " +
			"A `go_type:` value the generator cannot write is reported as Q049 instead.",
	},
	{
		Code:     CodeMutuallyExclusiveKeywordArgument,
		Title:    "Mutually exclusive keyword arguments",
		Severity: SeverityError,
		Explanation: "One directive call sets two keyword arguments that cannot be used together, such as `type:` " +
			"and `go_type:` on `sgl.column`. The diagnostic points at the second of the two.\n\n" +
			"Remove one of them.",
	},
	{
		Code:     CodeDirectiveLineLimit,
		Title:    "Directive header is too long",
		Severity: SeverityError,
		Explanation: "A query block's directive header has more lines than the parser will read, one directive " +
			"continues over more lines than the parser will join, or a query holds more inline sgl.embed markers " +
			"than the parser will read, so the rest was ignored. The limits are far above any realistic query and " +
			"exist to bound the work done on malformed input.\n\n" +
			"Check the file for a missing statement between headers, for runaway comment lines or for repeated " +
			"embed markers.",
	},
	{
		Code:     CodeAsyncExecRecommended,
		Title:    "Asynchronous mutation declared as exec",
		Severity: SeverityHint,
		Explanation: "An `exec` query runs a mutation the database applies asynchronously, such as ClickHouse's " +
			"`ALTER TABLE ... UPDATE` or `ALTER TABLE ... DELETE`. The call returns when the database accepts the " +
			"mutation, not when it has finished.\n\n" +
			"Declare the query with the `asyncexec` command so the generated method documents that behaviour and " +
			"callers do not assume the change is visible on return.",
	},
	{
		Code:     CodeAsyncExecNotSupported,
		Title:    "Asynchronous exec on a dialect without asynchronous mutations",
		Severity: SeverityError,
		Explanation: "A query uses the `asyncexec` command, but the dialect applies every mutation synchronously. " +
			"The command would quietly behave as `exec` and hide the author's intent.\n\n" +
			"Use `exec`, `execrows` or `execresult` instead.",
	},
	{
		Code:     CodeUnreferencedParameter,
		Title:    "Declared parameter is not used",
		Severity: SeverityError,
		Explanation: "A parameter binding declares a parameter whose placeholder never appears in the statement, " +
			"such as a binding for $3 in a statement that uses $1 and $2, so the generated method would take an " +
			"argument it ignores. It is an error, because the binding was most likely meant for another " +
			"placeholder. It is reported at the binding. A binding anchored at a name that spells a number, such as " +
			"`$1` on SQLite, which reads it as a parameter named 1 rather than the first placeholder, says how the " +
			"dialect writes a numbered placeholder, `?1` there.\n\n" +
			"Remove the binding, or use the placeholder in the statement.",
	},
	{
		Code:     CodeMigrationMixedTransaction,
		Title:    "Migration mixes transactional and non-transactional statements",
		Severity: SeverityWarning,
		Explanation: "A migration contains a statement that cannot run inside a transaction, marked with " +
			"`sgl.migration(no_transaction: true)` or detected automatically (such as PostgreSQL's " +
			"`CREATE INDEX CONCURRENTLY`), alongside other statements. One such statement makes the whole " +
			"migration run without a transaction, so a failure part-way leaves it half applied.\n\n" +
			"Move the non-transactional statement into its own migration file.",
	},
	{
		Code:     CodeDirectiveWrongContext,
		Title:    "Directive used in the wrong kind of file",
		Severity: SeverityWarning,
		Explanation: "A `sgl.migration` directive appears in a query file, or a `sgl.query` header appears in a " +
			"migration file. The directive is ignored.\n\n" +
			"Move the directive to the right kind of file, or remove it.",
	},
	{
		Code:     CodeFunctionDataAccessUndeclared,
		Title:    "Function data access not declared",
		Severity: SeverityWarning,
		Explanation: "The dialect resolved a function call without saying whether the function reads or modifies " +
			"data. Seagull assumes it only reads, which decides whether the generated query is marked read-only.\n\n" +
			"This points at a gap in the dialect rather than in your SQL. If the function modifies data, override " +
			"the read-only detection with `readonly: false` on the `sgl.query` header and report the gap to the " +
			"dialect's maintainers.",
	},
	{
		Code:     CodeConflictDoNothingReturning,
		Title:    "A conflict that skips the row, with RETURNING on a one query",
		Severity: SeverityWarning,
		Explanation: "A `one` query combines a conflict clause that skips the row, such as `ON CONFLICT DO NOTHING`, " +
			"MariaDB's `INSERT IGNORE` or SQLite's `INSERT OR IGNORE`, with `RETURNING`. When the conflict occurs the " +
			"insert is skipped and no row is returned, so the generated method returns the driver's no-rows error " +
			"(`sql.ErrNoRows`, or `pgx.ErrNoRows` for pgx) on that path.\n\n" +
			"Add `optional: true` to the header to receive `(row, false, nil)` instead, use `exec` or `execrows`, " +
			"or handle the no-rows error explicitly with `errors.Is`:\n\n" +
			"```sql\n" +
			"-- sgl.query(CreateTag, one, optional: true)\n" +
			"INSERT INTO tags (name) VALUES ($1) ON CONFLICT DO NOTHING RETURNING id;\n" +
			"```",
	},
	{
		Code:     CodeOptionalNonOneCommand,
		Title:    "Optional result requested where it does not apply",
		Severity: SeverityError,
		Explanation: "`optional: true` is set on a query where it has no meaning. It applies only to a static " +
			"`one` query, where it turns a missing row into `(row, false, nil)`; it is rejected on other commands, " +
			"on runtime builders and alongside optional predicate parameters rather than silently ignored.\n\n" +
			"Remove `optional`, or make the query a static `one` query.",
	},
	{
		Code:     CodeIgnoredMigrationFile,
		Title:    "Migration file ignored",
		Severity: SeverityWarning,
		Explanation: "A `.sql` file in the migrations directory is named neither `NNN_name.up.sql` nor " +
			"`NNN_name.down.sql`, so it was skipped.\n\n" +
			"When no file in the directory is an up migration the runner applies, such as a directory holding only " +
			"`001_schema.sql`, the same code is reported once, as an error, at the first skipped file: replaying the " +
			"directory would build an empty schema and type every column `any`, so nothing is generated until a " +
			"migration is named `NNN_name.up.sql`.\n\n" +
			"The same code reports two layouts of other migration tools that the runner would misread. A " +
			"subdirectory holding `.sql` files, as a directory-per-migration layout such as `01_init/up.sql` " +
			"keeps them, is not read at all. An up migration holding the down half of a single-file layout, " +
			"from a marker line such as goose's `-- +goose Down`, sql-migrate's `-- +migrate Down`, dbmate's " +
			"`-- migrate:down` or tern's `---- create above / drop below ----`, would be applied whole, the down " +
			"statements straight after the ones they undo, so it is an error at the marker, and the migration " +
			"runner refuses to apply such a file with the same message before it runs anything.\n\n" +
			"Rename it to follow the migration naming scheme, or move it out of the migrations directory. Move a " +
			"nested migration into the migrations directory as `NNN_name.up.sql`, and the statements after a down " +
			"marker into the migration's `NNN_name.down.sql`.",
	},
	{
		Code:     CodeUnusableGoTypeOverride,
		Title:    "Unusable Go type override",
		Severity: SeverityError,
		Explanation: "A `go_type` names a Go type the generator cannot write. It may come from " +
			"`sgl.column(..., go_type: ...)` in a migration or a query header, or from a type override in the " +
			"configuration. The override is not applied and nothing is generated until it is fixed.\n\n" +
			"The generator writes a type name, optionally qualified by the import path of its package, " +
			"optionally preceded by `*` or `[]` and optionally followed by type arguments in brackets, each " +
			"written the same way: `string`, `[]byte`, `time.Time`, `github.com/google/uuid.UUID`, " +
			"`github.com/x/opt.Option[uuid.UUID]`. A qualified name must be exported and the import path must " +
			"be valid. The generated code refers to the package by the name its path implies, its last element " +
			"(or the one before a `/vN` suffix) without a `go-` prefix and cut at the first character an " +
			"identifier cannot hold, so `gopkg.in/guregu/null.v4` is `null`; name a package whose name differs " +
			"in `go.types.package_names`. Composite types such as `map[string]int` are not accepted; declare a " +
			"named type for them and use that.\n\n" +
			"Write the type as its full import path followed by the type name:\n\n" +
			"```sql\n" +
			"-- sgl.column(users.id, go_type: \"github.com/google/uuid.UUID\")\n" +
			"```",
	},
	{
		Code:     CodeAliasPrefix,
		Title:    "Directive uses an alias prefix",
		Severity: SeverityHint,
		Explanation: "A directive is written with an alias of the canonical prefix, such as `seagull.query` where " +
			"`sgl.query` is canonical. It is parsed as usual; the hint appears only when the configuration requires " +
			"the canonical prefix, so a codebase can settle on one spelling.\n\n" +
			"Run `sgl fmt -w` to rewrite aliases to the canonical prefix.",
	},
	{
		Code:     CodeForeignPrefix,
		Title:    "Directive uses another tool's prefix",
		Severity: SeverityWarning,
		Explanation: "A comment looks like a directive but uses a prefix that belongs to another tool and is not " +
			"enabled for this run. The directive is not parsed, so the query may be generated without settings its " +
			"author intended.\n\n" +
			"To accept the directive as written, add the prefix to `directives.prefixes` in the configuration. " +
			"Listed after the canonical prefix it becomes an alias, so `sgl fmt -w` then rewrites each such " +
			"directive to the canonical prefix. To convert by hand instead, replace the prefix with the canonical " +
			"one. Do not reach for `sgl fmt --rewrite-prefix` here: it rewrites every directive to the prefix it " +
			"names, not from it.",
	},
	{
		Code:     CodeReturningNotSupported,
		Title:    "RETURNING clause on a dialect without RETURNING support",
		Severity: SeverityError,
		Explanation: "An `INSERT`, `UPDATE` or `DELETE` has a `RETURNING` clause, but the dialect declares that its " +
			"database has none. MySQL and ClickHouse, for example, do not accept `RETURNING`.\n\n" +
			"The database rejects the statement, so the generated method could only fail at run time. It is an " +
			"error rather than a warning because no command makes the statement work as written.\n\n" +
			"Remove the clause and declare an exec command. On an `INSERT`, `execresult` returns the driver's " +
			"result, which carries the generated key on drivers that report one; read anything else back with a " +
			"separate query:\n\n" +
			"```sql\n" +
			"-- sgl.query(CreateTask, execresult)\n" +
			"INSERT INTO tasks (title) VALUES (?);\n" +
			"```\n\n" +
			"If your server does accept `RETURNING`, as MariaDB does, choose the dialect that declares it, or set " +
			"the `returning` dialect option where the dialect offers one.",
	},
	{
		Code:     CodeDuplicateMigrationVersion,
		Title:    "Two migrations share a version",
		Severity: SeverityError,
		Explanation: "Two files in the migrations directory have the same version and direction, such as " +
			"`0003_add_orders.up.sql` and `0003_add_index.up.sql` after two branches merged. The migration runner " +
			"refuses to run either, so the generator reports it rather than build models the database never gets. " +
			"The catalogue replays only the first file of the version.\n\n" +
			"Give one of the files the next free version.",
	},
	{
		Code:     CodeDepthLimit,
		Title:    "Analysis depth limit reached",
		Severity: SeverityWarning,
		Explanation: "Part of a query, a view or a function body is nested more deeply than the analyser follows: " +
			"a long chain of casts, subqueries inside subqueries, scopes nested many levels deep, or a type " +
			"nested many levels inside arrays, maps or structs. The analyser stops at a fixed depth so a " +
			"pathological statement cannot exhaust memory, and the part beyond the limit is treated as unknown: " +
			"a column may lose its type or its source, and a function body may hide a call.\n\n" +
			"Flatten the statement, for example with a CTE, or check the generated code for the affected columns.",
	},
	{
		Code:     CodeSliceParameterShape,
		Title:    "Slice parameter has no list to fill",
		Severity: SeverityError,
		Explanation: "A parameter declared `kind: slice` takes a Go slice, whose elements the generated code writes " +
			"into an `IN` list. That needs the parameter's placeholder to be the only element of the list, and the " +
			"parameter's type to be the type of one element. The diagnostic fires when the placeholder is written " +
			"anywhere else, such as in `= ANY($1)` or `IN ($1, $2)`, or when its type is an array already, such as " +
			"a ClickHouse `{ids:Array(UInt64)}`, which would make each element a list.\n\n" +
			"Write the placeholder alone in the list:\n\n" +
			"```sql\n" +
			"-- $1 as sgl.param(ids, kind: slice)\n" +
			"SELECT id FROM users WHERE id IN ($1);\n" +
			"```\n\n" +
			"or, for a parameter that is an array already, leave out `kind: slice` and bind the array as one value.",
	},
	{
		Code:     CodeOptionalNotNullable,
		Title:    "Optional parameter cannot be NULL",
		Severity: SeverityError,
		Explanation: "A parameter declared `optional: true` lets the caller pass nil, which the query's " +
			"`(param IS NULL OR column = param)` idiom reads as \"no filter\". Its placeholder, though, carries a " +
			"type that cannot hold NULL, such as ClickHouse's `{status:String}`, so the database rejects a nil " +
			"value or compares nothing equal to it, and the query returns no rows instead of every row.\n\n" +
			"Give the placeholder a type that holds NULL:\n\n" +
			"```sql\n" +
			"-- {status:Nullable(String)} as sgl.param(status, optional: true)\n" +
			"SELECT id FROM orders WHERE ({status:Nullable(String)} IS NULL OR status = {status:Nullable(String)});\n" +
			"```",
	},
	{
		Code:     CodeSortableOrderByParameter,
		Title:    "Parameter in the ORDER BY of a sortable query",
		Severity: SeverityError,
		Explanation: "A sortable query's generated code drops the `ORDER BY` written in the query and writes the " +
			"ordering the caller chooses in its place. A parameter written in that `ORDER BY`, other than the " +
			"sortable placeholder itself, would go with it, so its value would never reach the database and the " +
			"remaining placeholders would no longer match the arguments.\n\n" +
			"Move the parameter out of the `ORDER BY`, or make the query not sortable and keep the ordering as " +
			"written.",
	},
	{
		Code:     CodeParametersBeforeStatement,
		Title:    "Placeholder before the query's statement",
		Severity: SeverityError,
		Explanation: "A query block holds more than one statement, and a statement before the query's own holds a " +
			"placeholder. The generated code binds values only to the query's own statement, so that placeholder " +
			"would reach the database unbound.\n\n" +
			"Give each statement its own query header, and bind parameters only in the statement the query runs.",
	},
	{
		Code:     CodeEmitterUnsupported,
		Title:    "Query or option the emitter cannot generate",
		Severity: SeverityError,
		Explanation: "The configured emitter cannot generate code for a query as written, or for one of its own " +
			"options, because its driver has no way to do what is asked. A query's command may need something the " +
			"driver does not report, such as `execrows` or `execresult` on a driver that returns no affected-row " +
			"count (clickhouse-go, behind database/sql or natively). An option may ask for something the driver " +
			"cannot honour, such as `emit_driver_databasesql.WithPrepared(true)` for ClickHouse, whose driver prepares only " +
			"`INSERT` statements and so cannot prepare the package's queries (`sgl` reports `go.prepared: true` " +
			"there as a configuration error before it generates). A problem with a query is reported at the query, " +
			"and one with an option against the package. The message names the emitter's reason. `sgl vet` reports " +
			"it as `sgl generate` does.\n\n" +
			"A dialect may also refuse a column whose value no driver it runs on can " +
			"read, such as a DuckDB STRUCT built by ROW(a, b), whose fields have no names; the error points at the " +
			"select-list item.\n\n" +
			"Choose a command the emitter supports, leave out the option the driver cannot honour, name the fields " +
			"of a value the driver cannot read, or choose an emitter whose driver supports what the query needs.",
	},
	{
		Code:     CodeBatchShape,
		Title:    "Batch query cannot be repeated per row",
		Severity: SeverityError,
		Explanation: "A `batch` or `copyfrom` query sends many rows at once. Where the emitter sends them as one " +
			"statement, as database/sql and the native ClickHouse batch do, the generated code repeats the " +
			"statement's `VALUES` row once per row, which needs an `INSERT` with exactly one `VALUES` row and every " +
			"parameter inside that row; an emitter that sends the rows as data, such as a copy protocol, also " +
			"needs each item of the row to be a placeholder alone. pgx queues one statement per item for a " +
			"`batch`, which takes any statement.\n\n" +
			"Rewrite the statement as an `INSERT ... VALUES (...)` of one row. Refer to the inserted values in an " +
			"`ON CONFLICT` or `ON DUPLICATE KEY` clause through the engine's own form, such as `EXCLUDED.column` or " +
			"`VALUES(column)`, rather than a further parameter.",
	},
	{
		Code:     CodeUnmatchedTypeOverride,
		Title:    "Type override matches no type",
		Severity: SeverityWarning,
		Explanation: "A type override for a SQL type names a type the dialect does not know, and no column of the " +
			"schema has a type of that name, so the override changes no Go type. A misspelt name, or a name " +
			"qualified by a schema the dialect keeps its own types in, does this.\n\n" +
			"Name the type as the dialect spells it in a migration's column types, or as the schema's enum, " +
			"composite or domain is named, or remove the override.",
	},
	{
		Code:     CodeUngroupedColumn,
		Title:    "Column outside every aggregate of a query without GROUP BY",
		Severity: SeverityError,
		Explanation: "A query whose select list calls an aggregate, such as `count(*)`, and has no `GROUP BY` " +
			"clause returns one row, however many rows it reads. A column the select list reads outside every " +
			"aggregate then has no single value, and the engine decides what to do with it. PostgreSQL, " +
			"CockroachDB, TimescaleDB, DuckDB and ClickHouse reject the query, so Seagull reports an error. MySQL " +
			"rejects it in its default mode, whose `sql_mode` holds `ONLY_FULL_GROUP_BY`, and otherwise returns " +
			"the value of one of the rows read, or NULL when the query reads no row, so Seagull reports a warning " +
			"and types the column nullable. MariaDB and SQLite return the row in their default mode, so Seagull " +
			"types the column nullable and reports nothing.\n\n" +
			"Group by the column, read it through an aggregate such as `max(column)`, or select it in a query of " +
			"its own.",
	},
	{
		Code:     CodeEngineReading,
		Title:    "SQL the engine reads differently",
		Severity: SeverityWarning,
		Explanation: "The dialect warns that its engine runs part of the statement but reads it other than the " +
			"statement appears to mean. In the default SQL mode of MySQL and MariaDB, `||` is a logical OR, as " +
			"`OR` is, not the string concatenation it is in standard SQL and on the other engines, so " +
			"`name LIKE '%' || ? || '%'` reads as `(name LIKE '%') OR ? OR '%'`, which every name matches, and the " +
			"query returns every row. Seagull types the operator as the engine reads it and reports this warning where an " +
			"operand of `||` is text by its form, such as a string literal. SQLite reads a name in double quotes " +
			"that names no column, as in `title LIKE concat(\"%\", ?, \"%\")`, as a string, which a build of " +
			"SQLite without double-quoted strings rejects instead, so Seagull types it as text and reports this " +
			"warning; write strings in single quotes. DuckDB ignores the alias of a parenthesised join whose last " +
			"join merges columns with `USING`, as in `(a JOIN b USING (id)) AS r`, so the tables inside keep their " +
			"own names and `r` names nothing; join `ON` a condition to name the join. On PostgreSQL, CockroachDB and " +
			"TimescaleDB, `@name` is the absolute value of the column `name`, or a syntax error, and `?` the operator " +
			"that tests whether a jsonb document has a key, so a placeholder written in another tool's syntax is no " +
			"parameter; write `$1` or `:name`. The server also types each parameter where it first meets it, so a " +
			"parameter met first where nothing gives it a type makes every call fail: on PostgreSQL and TimescaleDB, " +
			"the operand of `IS NULL`, an argument of a function taking any type such as `concat`, either operand of " +
			"arithmetic between two parameters, or one in an `OFFSET` such as `OFFSET :size * :page`, which the server " +
			"types before the `LIMIT`; on CockroachDB, which types a parameter from what its place wants, the operand " +
			"of `IS NULL`. Cast it there, as in `$1::text IS NULL`.\n\n" +
			"Write the statement as the engine reads it: join strings with `CONCAT`, and write `OR` for a " +
			"logical OR:\n\n" +
			"```sql\n" +
			"-- sgl.query(FindPeople, many)\n" +
			"SELECT id, name FROM people WHERE name LIKE CONCAT('%', ?, '%');\n" +
			"```",
	},
	{
		Code:     CodeInsertShape,
		Title:    "INSERT shape the engine refuses",
		Severity: SeverityError,
		Explanation: "An `INSERT` gives each target column one value. A `VALUES` row that gives more or fewer values " +
			"than the statement lists target columns, a row with more values than the table has columns where the " +
			"statement lists none, and an `INSERT ... SELECT` whose select list gives another number of columns " +
			"than the list names are refused by the engine, so every call of the generated method fails.\n\n" +
			"Give each row one value per listed column, list the columns the values are for, or select exactly " +
			"the listed number of columns:\n\n" +
			"```sql\n" +
			"-- sgl.query(CreateAuthor, exec)\n" +
			"INSERT INTO authors (name, bio) VALUES ($1, $2);\n" +
			"```",
	},
	{
		Code:     CodeRowType,
		Title:    "Rows that do not fit the struct the query names",
		Severity: SeverityError,
		Explanation: "A query header's `model:` keyword reads the query's rows into the model of a table, and `row:` " +
			"into a struct of that name, which every query naming it shares, instead of a Row struct of the " +
			"query's own. Seagull reads the rows into such a struct only when the Row struct the query would " +
			"declare has its fields: the same field names, each of the same Go type, nullability included, and " +
			"with the same struct tags, in any order. The first query naming a row type, by file and then by " +
			"name, declares it from its own columns, and every other query naming it must give the same fields. " +
			"The message lists each field the struct has that no column gives, each column the struct has no " +
			"field for, and each column of another Go type or other struct tags.\n\n" +
			"The code also reports `model:` naming a table the schema does not hold, or a view, which has no " +
			"model; `model:` or `row:` on a query that returns no rows, or that embeds a table with `sgl.embed`; " +
			"and a `row:` name the generated package already declares, such as a model's.\n\n" +
			"Select the columns the struct holds, aliased to its fields' names and cast to its types, or drop " +
			"the keyword:\n\n" +
			"```sql\n" +
			"-- sgl.query(ListRoles, many, model: roles)\n" +
			"SELECT id, display_name, description FROM roles;\n" +
			"```",
	},
	{
		Code:     CodeDiagnosticLimit,
		Title:    "Further diagnostics left out",
		Severity: SeverityWarning,
		Explanation: "Seagull stops listing diagnostics once it reaches a limit, so a file with thousands of problems, " +
			"such as a query file written for another dialect, cannot flood the output or exhaust memory. It counts " +
			"the diagnostics it leaves out and reports this one in their place, saying how many there were, as in " +
			"`and 1234 more diagnostics were left out`.\n\n" +
			"This diagnostic takes the severity of the most severe diagnostic it stands for: it is an error when an " +
			"error was left out, so a run that stops listing early fails exactly as a full run would.\n\n" +
			"Fix the diagnostics that are listed and run again; the ones left out appear as the list shrinks. When " +
			"most of them share one cause, such as a file read with the wrong dialect, fix that cause first.",
	},
	{
		Code:     CodeDuplicateObject,
		Title:    "Object already exists",
		Severity: SeverityError,
		Explanation: "A migration statement creates a table under a name a table in the same schema already " +
			"holds, adds a column or an enum value that is already there, or renames or moves a table, view, type, " +
			"sequence, column or enum value onto a name already taken, as `ALTER TABLE orders RENAME TO invoices` " +
			"does while a table `invoices` exists. The engine refuses the statement, so the migration fails when it " +
			"runs, and Seagull does not apply it to the schema it builds: the existing object keeps its name and its " +
			"columns. A view, sequence, type or function created twice is not checked.\n\n" +
			"Choose a name no object in the schema holds, drop the existing object first, or write `IF NOT EXISTS` " +
			"where the statement should do nothing when the object is already there:\n\n" +
			"```sql\n" +
			"CREATE TABLE IF NOT EXISTS invoices (id BIGINT PRIMARY KEY);\n" +
			"```",
	},
	{
		Code:     CodeUnknownObject,
		Title:    "Object to change does not exist",
		Severity: SeverityError,
		Explanation: "A migration statement alters, drops or renames an object that no earlier migration creates, " +
			"or one of another kind than the statement names, as `DROP VIEW orders` does when `orders` is a table. " +
			"Renaming a column the table does not have, or an enum value the type does not hold, does the same. The " +
			"engine refuses the statement, so the migration fails when it runs, and Seagull does not apply it to " +
			"the schema it builds.\n\n" +
			"Check the spelling of the name and its schema, check that the migration creating the object has a " +
			"lower version, and use the statement for the object's kind. Write `IF EXISTS` where the statement " +
			"should do nothing when the object is missing:\n\n" +
			"```sql\n" +
			"DROP TABLE IF EXISTS legacy_orders;\n" +
			"```",
	},
	{
		Code:     CodePartlyAnalysed,
		Title:    "Part of a statement not analysed",
		Severity: SeverityWarning,
		Explanation: "Seagull could not analyse part of a statement, such as the body of a view or of a DuckDB table " +
			"macro, because the part uses a construct the analyser does not follow or is nested more deeply than " +
			"it follows. The rest of the statement still applies to the schema Seagull builds, but the columns the " +
			"part defines are unknown: a view that lists its column names, as in `CREATE VIEW active_users (id, " +
			"name) AS ...`, keeps those names without their types, and one that does not has no columns. Queries " +
			"that read it may then report unknown columns (Q001) or produce fields without a precise type. The " +
			"message names the part and says why it was not analysed.\n\n" +
			"Rewrite the part with constructs the analyser follows, or flatten it, for example with a CTE. Where " +
			"that is not possible, list the view's columns in its definition and pin the types of the columns a " +
			"query reads with `sgl.column`:\n\n" +
			"```sql\n" +
			"-- sgl.query(ListActiveUsers, many)\n" +
			"-- sgl.column(name, type: text)\n" +
			"SELECT id, name FROM active_users;\n" +
			"```",
	},
	{
		Code:     CodeRecoveredPanic,
		Title:    "Panic recovered while handling a statement",
		Severity: SeverityError,
		Explanation: "Seagull, the dialect, or code the host program supplies, such as a custom dialect, emitter or " +
			"hook, panicked while handling a statement or a query. Seagull recovered from the panic, logged its " +
			"stack once as a warning, and reported this error in place of the result, so the rest of the run " +
			"carries on. The statement is left out: a migration statement does not change the schema Seagull " +
			"builds, and a query gets no generated code.\n\n" +
			"A panic is a bug in the code that panicked rather than a problem with the SQL, though unusual SQL is " +
			"what usually reaches it. Run `sgl` again with `--log-level warn` to see the stack, or read it from the " +
			"logger given to `WithLogger` when embedding the library, and report it with the statement and the " +
			"dialect. Until it is fixed, rewrite the statement the message points at, or move the code that " +
			"panicked out of the run.",
	},
	{
		Code:     CodeViewDerivationUnsettled,
		Title:    "View columns did not settle",
		Severity: SeverityWarning,
		Explanation: "On SQLite and DuckDB a view keeps the query that defines it rather than the columns it had when " +
			"it was created, so changing a table it reads changes the view's columns. After each change to a table " +
			"or view, Seagull derives the columns of such views again, and repeats while any view's columns still " +
			"change, so that a view reading another view follows too. It does the same for the views of a schema " +
			"read from a database, typing them from their definitions. It stops after a fixed number of rounds, so " +
			"views whose columns keep changing cannot hold up the run, and this warning says it stopped before " +
			"every view settled: the columns of some views may not match those the engine reports.\n\n" +
			"Look for views that read each other in a cycle, directly or through other views, and break the cycle. " +
			"Shorten long chains of views that read views, or drop and create the affected views again after the " +
			"change to their tables, so their columns are derived from the tables as they now are.",
	},
}

// Codes returns the documentation of every diagnostic code in ascending order.
//
// Returns []CodeInfo which is a fresh copy the caller may modify.
func Codes() []CodeInfo {
	return slices.Clone(codeTable)
}

// LookupCode returns the documentation of one diagnostic code.
//
// Takes code (string) which names the code in any case and with or without the leading
// letter and zero padding, such as "Q012", "q012", "Q12" or "12".
//
// Returns CodeInfo which documents the code.
// Returns bool which is false when no code matches.
func LookupCode(code string) (CodeInfo, bool) {
	normalised, ok := normaliseCode(code)
	if !ok {
		return CodeInfo{Code: "", Title: "", Explanation: "", Severity: SeverityError}, false
	}
	index := slices.IndexFunc(codeTable, func(entry CodeInfo) bool { return entry.Code == normalised })
	if index < 0 {
		return CodeInfo{Code: "", Title: "", Explanation: "", Severity: SeverityError}, false
	}
	return codeTable[index], true
}

// normaliseCode rewrites a user-supplied code to the canonical "Qnnn" form.
//
// Takes code (string) which is the code as the user wrote it.
//
// Returns string which is the canonical code.
// Returns bool which is false when the input is not a code.
func normaliseCode(code string) (string, bool) {
	digits := strings.TrimSpace(code)
	if rest, found := strings.CutPrefix(strings.ToUpper(digits), codePrefix); found {
		digits = rest
	}
	if digits == "" || strings.ContainsFunc(digits, func(character rune) bool { return character < '0' || character > '9' }) {
		return "", false
	}
	number, err := strconv.Atoi(digits)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%s%03d", codePrefix, number), true
}
