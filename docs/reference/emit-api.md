---
title: Emit API
description: "sdk/emit is the contract a code emitter implements: it turns analysed queries, their naming plan and resolved types into Go files for one driver API."
section: Reference
subsection: Extensions
order: 1060
---

# Emit API

An emitter turns the queries Seagull has analysed into Go source written against one
database driver API, and the generator calls it once for each package it writes. The emit
area of the `sdk` kit, `seagull.sh/seagull/sdk/emit`, is its contract: an emitter
translates analysed queries, their naming plan and resolved types into named Go files.
`emit.CheckerPort` reports unsupported requests as diagnostics before generation;
`emit.EmitterPort` produces the files. The same package holds the query and Go-type half
of the model, listed in [SQL model](sql-model.md). The dialect's facts come from
`seagull.sh/seagull/sdk/dialect`: `emit.Request.Dialect` is a `dialect.Facts`, and the
optional ports that ask about the dialect take a `*dialect.Facts`.

The [emitter guide](../how-to/write-an-emitter.md) describes implementation and
verification; this page records the public API.

## Providers

The built-in emitters, and the tools for writing and testing one, are provider packages
under `sdk/emit`, each imported as `seagull.sh/seagull/sdk/emit/<sub-package>`. Pass an
emitter to `seagull.WithEmitter`, or choose it with `go.driver` in `sgl.yaml`.

| Sub-package | What it provides | When to use it |
|---|---|---|
| `emit_driver_databasesql` | The emitter for the standard `database/sql` interfaces: `New`, `WithDriver`, `WithPrepared` and `PreparesStatements` | The default and the portable choice: every dialect, any `database/sql` driver |
| `emit_driver_pgx` | The emitter for pgx v5's native interfaces | A PostgreSQL-family database the application reaches through pgx, for `COPY`, pgx batches and array parameters |
| `emit_driver_clickhouse` | The emitter for clickhouse-go v2's native interface | ClickHouse through the native driver, for its columnar batches |
| `emit_kit` | The building blocks the built-in emitters share: rendering a `SQLTemplate` for a driver, the placeholder form a query executes in, batch and type-depth checks, row templates for a multi-row `INSERT`, and the names an emitter reserves | Writing an emitter |
| `emit_testkit` | The conformance suite: `Run`, `Suite`, `Corpus` and the `DatabaseSQL` runtime harness | Testing an emitter |

The `database/sql` emitter generates a prepared wrapper by default, whereas CLI
preparation is opt-in. Constructor options describe these driver and output choices.

Source: [`sdk/emit/facade.go`](../../sdk/emit/facade.go).

## Exported package inventory

These declarations are the current `go doc -short` output of each package. Most are
aliases of the implementation under `internal/`, which a program never imports: run
`go doc` on a name for its doc comment, and see the [key declarations](#key-declarations)
for the fields of the main types.

### The emitter ports

[Package source](../../sdk/emit) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/emit)

`go doc -short` folds a constant group into its first name: the query commands
(`QueryCommandOne` and the rest), the segment kinds (`SegmentText` and the rest), the
parameter directives (`ParameterDirectiveNone`, `ParameterDirectiveParam` and
`ParameterDirectiveSortable`) and the severities (`SeverityError`, `SeverityWarning` and
`SeverityHint`).

```go
const QueryCommandOne = sqlmodel.QueryCommandOne ...
const SegmentText = sqlmodel.SegmentText ...
const ParameterDirectiveNone = sqlmodel.ParameterDirectiveNone ...
const SeverityError = diag.SeverityError ...
const CodeEmitterUnsupported = diag.CodeEmitterUnsupported
const InlineJSONTypeName = emit.InlineJSONTypeName
type AllowedColumn = sqlmodel.AllowedColumn
type AnalysedQuery = sqlmodel.AnalysedQuery
type CheckerPort = emit.CheckerPort
type EmitterPort = emit.EmitterPort
type ErrorSeverity = diag.ErrorSeverity
type GeneratedFile = sqlmodel.GeneratedFile
type GoType = sqlmodel.GoType
type GoTypeResolver = sqlmodel.GoTypeResolver
type ModelNaming = emit.ModelNaming
type NamedType = emit.NamedType
type Naming = emit.Naming
type NullElementBinderPort = emit.NullElementBinderPort
type OutputColumn = sqlmodel.OutputColumn
type ParameterDirectiveKind = sqlmodel.ParameterDirectiveKind
type Problem = emit.Problem
type QueryCommand = sqlmodel.QueryCommand
type QueryNaming = emit.QueryNaming
type QueryParameter = sqlmodel.QueryParameter
type Request = emit.Request
type Reservation = emit.Reservation
type ReserverPort = emit.ReserverPort
type SQLSegment = sqlmodel.SQLSegment
type SQLSegmentKind = sqlmodel.SQLSegmentKind
type SQLTemplate = sqlmodel.SQLTemplate
type Style = emit.Style
type TypeDefaulterPort = emit.TypeDefaulterPort
type TypeSlot = sqlmodel.TypeSlot
type TypeSlots = sqlmodel.TypeSlots
```

### emit_kit

[Package source](../../sdk/emit/emit_kit) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/emit/emit_kit)

`Render` writes an executable template and its binding order, and `ExecutionForm` chooses
the placeholder form it executes in. `CheckTypeDepth` and `CheckBatch` refuse what the
generated code could not serve; `RowTemplate`, `RowColumns` and `ClausesAfterValues` help
send the rows of a multi-row `INSERT` as a batch. `DefaultReservation` and
`ReservationFor` give the names an emitter's code reserves, `MethodName` the method name a
query's name becomes, and `CommentLine` the comment line that tells apart two queries
whose SQL is written alike. `ClausesAfterValues` takes the dialect's lexical rules,
`request.Dialect.Lexical`, whose type `sdk/migrate` names `Rules`. The
[emitter guide](../how-to/write-an-emitter.md) shows them in use.

```go
const ListExpand = emitkit.ListExpand ...
const ColumnWrapJSON = emitkit.ColumnWrapJSON ...
const BatchRows = emitkit.BatchRows ...
func CheckBatch(query *emit.AnalysedQuery, shape BatchShape, limits ...BindLimit) []emit.Problem
func CheckTypeDepth(request *emit.Request) []emit.Problem
func ClausesAfterValues(query *emit.AnalysedQuery, rules *sqllex.Rules) bool
func CommentLine(info *dialect.Facts, text string) string
func DefaultReservation(info *dialect.Facts, style *emit.Style) emit.Reservation
func ExecutionForm(info *dialect.Facts, query *emit.AnalysedQuery) dialect.PlaceholderSigil
func MethodName(name string) (string, error)
func ReservationFor(emitter emit.EmitterPort, info *dialect.Facts, style *emit.Style) emit.Reservation
func RowColumns(query *emit.AnalysedQuery) ([]int, bool)
type BatchShape = emitkit.BatchShape
type BindLimit = emitkit.BindLimit
type ColumnWrap = emitkit.ColumnWrap
type ColumnWrapKind = emitkit.ColumnWrapKind
type ListMode = emitkit.ListMode
type Marks = emitkit.Marks
type Rendered = emitkit.Rendered
    func Render(template emit.SQLTemplate, renderer *Renderer) (Rendered, error)
type Renderer = emitkit.Renderer
type Row = emitkit.Row
    func RowTemplate(query *emit.AnalysedQuery, renderer *Renderer) (Row, error)
type SegmentSet = emitkit.SegmentSet
    func NewSegmentSet(kinds ...emit.SQLSegmentKind) SegmentSet
```

### emit_driver_databasesql

[Package source](../../sdk/emit/emit_driver_databasesql) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/emit/emit_driver_databasesql)

```go
func New(options ...Option) emit.EmitterPort
func PreparesStatements(info *dialect.Facts, driver string) bool
type Option = databasesql.Option
    func WithDriver(driver string) Option
    func WithPrepared(enabled bool) Option
```

### emit_driver_pgx

[Package source](../../sdk/emit/emit_driver_pgx) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/emit/emit_driver_pgx)

```go
func New() emit.EmitterPort
```

### emit_driver_clickhouse

[Package source](../../sdk/emit/emit_driver_clickhouse) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/emit/emit_driver_clickhouse)

```go
func New() emit.EmitterPort
```

### emit_testkit

[Package source](../../sdk/emit/emit_testkit) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/emit/emit_testkit)

The emitter conformance suite. `Run` drives an emitter through the generator with each dialect a `Suite` names, over a neutral corpus written in each dialect's own syntax, and checks what every emitter must guarantee. It also runs the martian dialect, an invented engine that shares no convention with a real database (upper-case folding, bracket quoting, `@p1` placeholders, types of its own and `OFFSET`-first pagination), so every emitter is shown to assume no real engine; a suite that lists a dialect of that name runs it once. A `Harness` adds a runtime lane that builds the generated code with a recording stand-in for the driver and checks what each method sends: `DatabaseSQL` is the harness for code written against `database/sql`, and a `Harness` of the emitter's own stands in for any other driver API. `Suite.KnownFailures` records the checks an emitter fails today, each with its reason, and each must still fail; `Run` stops the test before any check runs when an entry gives no reason or names no check or no dialect of the suite, the martian dialect included, or when the suite lists no dialect. A generated package that does not build with the harness fails the runtime checks of its case with the compiler's report. When a check, an emitter or its constructor panics, that check fails and the stack is logged once in the subtest's output. The lane's `go build` keeps the environment's `GOFLAGS` and adds `-buildvcs=false` and `-mod=mod`.

```go
func Run(t *testing.T, suite Suite)
type Corpus = emittest.Corpus
type Harness = emittest.Harness
    func DatabaseSQL() *Harness
type Suite = emittest.Suite
```

## Key declarations

These declarations hold the contract an emitter works to. `go doc` shows each as an alias
of its declaration under `internal/`; the second block of each is that declaration,
written with the names its package exports. A type the facades leave unnamed, such as
`EmbedNaming`, `Range` or the `dialect.ListSyntax` a `Renderer` copies from `Facts`, keeps
its internal name: an emitter reads its value where the declaration gives it and never
names it.

### The request

```go
package emit // import "seagull.sh/seagull/sdk/emit"

type Request = emit.Request
    Request carries everything one emission needs: the package name,
    the catalogue, the analysed queries, the type mapping, the dialect's facts,
    the style of the generated code and the naming plan. An emitter must not
    change it.
```

The declaration `Request` aliases, written with the names `sdk/emit` exports:

```go
type Request struct {
	// Catalogue holds the schema the models are generated from.
	Catalogue *dialect.Catalogue

	// Types maps SQL types to Go types using configured overrides, slots and defaults. A
	// column or parameter's explicit GoTypeOverride takes precedence.
	Types GoTypeResolver

	// Package is the Go package name of the generated files.
	Package string

	// Style controls the names, header and optional files of the generated code.
	Style Style

	// Queries holds the analysed queries, none nil, in the order they were read. The output
	// must not depend on that order: where order shows, sort by Filename and Name, as the
	// naming plan does.
	Queries []*AnalysedQuery

	// DirectivePrefixes lists the directive prefixes the queries were written with,
	// canonical first, such as sgl, so a message can name a directive the way the user
	// writes it, such as sgl.embed. The SQL to embed never holds a directive line: each
	// query's Template leaves them out.
	DirectivePrefixes []string

	// Naming supplies the package's planned declaration, file and import names. It is zero
	// when the caller supplied no plan.
	Naming Naming

	// Dialect holds the facts of the SQL dialect the queries were analysed with, such as how
	// parameters are written and identifiers quoted. Emitters choose their driver's
	// behaviour from its Drivers, never from its name or family.
	Dialect dialect.Facts
}
```

### The names an emitter reserves

```go
package emit // import "seagull.sh/seagull/sdk/emit"

type Reservation = emit.Reservation
    Reservation lists the names an emitter's generated code uses whatever the
    queries are.

    It reserves every name the emitter may write, not only those one package
    needs, so the plan for a package does not depend on which helper files it
    gets. A ReserverPort returns one; emit_kit.DefaultReservation is the one an
    emitter without it gets.
```

The declaration `Reservation` aliases, written with the names `sdk/emit` exports:

```go
type Reservation struct {
	// Receiver is the receiver of the Queries methods, such as queries.
	Receiver string

	// Locals reserves body-local names that planned arguments, column locals and SQL
	// constants must avoid.
	Locals []string

	// Imports lists the import paths the generated files may use, in the order their
	// qualifiers are claimed: of two paths implying the same package name, the first keeps
	// it.
	Imports []string

	// Declarations lists the package-level identifiers the emitter declares, such as
	// Queries, New, DBTX and every helper.
	Declarations []string

	// Methods lists the methods and fields the emitter declares on Queries besides one
	// method per query, such as WithTx and reader.
	Methods []string

	// Files lists the names of the files the emitter writes besides those holding queries,
	// such as querier.go.
	Files []string
}
```

### A query's names

```go
package emit // import "seagull.sh/seagull/sdk/emit"

type QueryNaming = emit.QueryNaming
    QueryNaming holds the names one query's generated code declares and uses.

    A name the query does not need is empty, and a slice it does not need is
    nil. A slice the query needs is exactly as long as the slice of the query it
    is parallel to.
```

The declaration `QueryNaming` aliases, written with the names `sdk/emit` exports:

```go
type QueryNaming struct {
	// Method is the Queries method that runs the query, such as GetUser: a snake_case query
	// name becomes PascalCase with common initialisms; other names have their first letter
	// upper-cased. The name is made unique among the Queries methods.
	Method string

	// SQLConstant is the package-level constant holding the query's SQL: the query's name in
	// lower camel case (as Style.SQLConstantLowerCase says) followed by
	// Style.SQLConstantSuffix, such as getUserSQL, made unique in the package.
	SQLConstant string

	// CountConstant is the package-level constant holding the SQL that counts the rows a
	// runtime builder query returns, for a query whose rows can be counted.
	CountConstant string

	// Label is the name the query-names lookup reports for the query's SQL, such as GetUser.
	// It labels telemetry, so it is not made unique.
	Label string

	// Params is the struct type holding the query's parameters, such as GetUserParams, or
	// empty when the generated method accepts its parameters as arguments of their own. A
	// batch or copyfrom method takes a slice of it.
	Params string

	// ParamsArgument is the method argument holding the Params struct, or the slice of them
	// for a batch or copyfrom method, such as params.
	ParamsArgument string

	// Row is the struct type a result row is read into, such as GetUserRow, for a query
	// returning rows.
	Row string

	// Builder is the runtime builder type of a query generated as one.
	Builder string

	// AllowedColumns is the package-level variable mapping each column a runtime builder
	// accepts to the SQL it writes for it.
	AllowedColumns string

	// OrderBy is the string type naming the columns a sortable query orders by.
	OrderBy string

	// DirectionField is the Params field holding the direction a sortable query orders in,
	// such as SortDirection.
	DirectionField string

	// ParameterFields holds, parallel to AnalysedQuery.Parameters, the Params field each
	// parameter is read from, or the name of the method argument holding it when there is no
	// Params struct.
	ParameterFields []string

	// ColumnFields holds, parallel to AnalysedQuery.OutputColumns, the field each column is
	// read into: a field of Row, or for an embedded column a field of its embed's struct.
	ColumnFields []string

	// ColumnLocals holds, parallel to AnalysedQuery.OutputColumns, the local variable each
	// column is scanned into before the rows are grouped, for a grouped query.
	ColumnLocals []string

	// Embeds names the struct of each embed group the query's rows hold, in the order the
	// groups first appear among the output columns.
	Embeds []EmbedNaming

	// OrderByConstants holds, parallel to the SortableColumns of the query's sortable
	// parameter, the OrderBy constant naming each column.
	OrderByConstants []string

	// SharedRow reports that Row names a table model or shared row type declared elsewhere.
	// ColumnFields and the query's column types describe the fields used by this query.
	SharedRow bool
}
```

### Refusals

```go
package emit // import "seagull.sh/seagull/sdk/emit"

type Problem = emit.Problem
    Problem is something an emitter or a code generation tool finds wrong with
    a query or a request, reported as a diagnostic rather than as an error from
    Emit.

    A problem names its query, or none for the request as a whole, and is
    reported under CodeEmitterUnsupported for what the driver cannot serve.
    A SeverityError problem stops emission.
```

The declaration `Problem` aliases, written with the names `sdk/emit` exports:

```go
type Problem struct {
	// Query is the query the problem is about, one of Request.Queries, whose Filename and
	// Line the diagnostic is reported at, or nil for a problem with the request as a whole,
	// such as an emitter option the driver cannot honour.
	Query *AnalysedQuery

	// Code is the diagnostic code the problem is reported under: emit.CodeEmitterUnsupported
	// (Q059) for a query or option the driver cannot serve, and seagull.CodeBatchShape
	// (Q060) for a batch the emitter cannot send, as emit_kit.CheckBatch reports it.
	Code string

	// Message describes the problem, and what to change, in a sentence.
	Message string

	// Severity says whether the problem stops emission (emit.SeverityError) or is reported
	// only.
	Severity ErrorSeverity
}
```

### Rendering

```go
package emit_kit // import "seagull.sh/seagull/sdk/emit/emit_kit"

type Renderer = emitkit.Renderer
    Renderer says how Render writes a template for one driver: the placeholder
    form, how slice lists are written, which output columns are wrapped and
    which clauses are left out. One renderer serves one query, whose parameters
    and output columns the template's indices refer to.
```

The declaration `Renderer` aliases, written with the names `sdk/emit/emit_kit` exports:

```go
type Renderer struct {
	// WrapColumn returns how to wrap the output column at an index, or false to leave it as
	// it is. A nil WrapColumn wraps no column.
	WrapColumn func(column int) (ColumnWrap, bool)

	// Query is the query the template belongs to. Render reads the names of its parameters,
	// for a named form, and of its output columns, for the alias a wrapped column keeps.
	Query *emit.AnalysedQuery

	// Prefix is written before the SQL as it is, such as a comment line naming the query,
	// which tells apart the SQL of two queries that would otherwise be written alike. The
	// marks count it, and it binds nothing.
	Prefix string

	// Quote delimits the identifiers Render writes itself: the columns of an expanded star
	// and the alias of a wrapped column.
	Quote dialect.IdentifierQuote

	// Lists holds the dialect's array forms, which ListArray writes.
	Lists dialect.ListSyntax

	// Form is the form every placeholder is written in: the dialect's Execution form, or its
	// positional form where the generated code binds by position. ExecutionForm gives the
	// one a query executes in.
	Form dialect.PlaceholderSigil

	// Placeholders is the dialect's placeholder description, whose NamePrefix a named form
	// binds under.
	Placeholders dialect.Placeholders

	// Omit lists ORDER BY or pagination segments to suppress while retaining their insertion
	// marks. Suppressed segments contribute no binds.
	Omit SegmentSet

	// ListMode says how slice lists are written.
	ListMode ListMode

	// KeepStars preserves stars unless the renderer wraps a column. By default stars expand
	// to the generated columns to keep reads stable after migrations add columns, except
	// stars marked AsWritten by the template.
	KeepStars bool
}
```

```go
package emit_kit // import "seagull.sh/seagull/sdk/emit/emit_kit"

type Marks = emitkit.Marks
    Marks locates in Rendered.SQL the parts the generated code edits at run
    time. A mark for a segment the template does not hold is nil; a template
    holds at most one filter, ORDER BY, pagination and VALUES row segment.
```

The declaration `Marks` aliases, written with the names `sdk/emit/emit_kit` exports:

```go
type Marks struct {
	// Wrapped maps the index of each output column Render wrapped to how it is read, and is
	// nil when no column was wrapped.
	Wrapped map[int]ColumnWrapKind

	// Filter locates the filter segment.
	Filter *FilterMark

	// OrderBy covers the ORDER BY clause, or marks its place when the clause is absent or
	// omitted.
	OrderBy *Range

	// Pagination covers the LIMIT, OFFSET and FETCH clauses, or marks their place when they
	// are absent or omitted.
	Pagination *Range

	// Values covers the VALUES row, from its opening parenthesis through its closing one.
	Values *Range

	// Lists locates each slice parameter's list, in the order they are written.
	Lists []ListMark
}
```

### Batch rows

```go
package emit_kit // import "seagull.sh/seagull/sdk/emit/emit_kit"

type Row = emitkit.Row
    Row is the template of one row of a multi-row INSERT, read from a query's
    VALUES row, which the generated code writes once per row it batches,
    after the SQL before the row and before the SQL after it.

    A row writes Parts in order with a place between each two. In a numbered
    form a place takes the number, in the statement, of the argument Slots
    names: the row's index times the number of Arguments, plus the slot, plus
    one. In a named form it takes the row's index, which ends the names of the
    row's arguments, so each row binds its own. An anonymous form has no places,
    so its whole row is the one part, binding Arguments in order.

func RowTemplate(query *emit.AnalysedQuery, renderer *Renderer) (Row, error)
```

The declaration `Row` aliases, written with the names `sdk/emit/emit_kit` exports:

```go
type Row struct {
	// Parts holds the row's text around its places, one more than Slots, with each
	// placeholder's prefix, name and type written around its place.
	Parts []string

	// Slots holds, for each place, the index into Arguments of the argument written there,
	// so a placeholder written twice reuses its argument.
	Slots []int

	// Arguments holds, in the order a row binds them, the index into
	// AnalysedQuery.Parameters of each argument: each parameter once, in the template's
	// canonical number order, for a numbered or named form, and one argument per
	// placeholder, in the order they are written, for an anonymous form.
	Arguments []int

	// Names holds, parallel to Arguments for a named form, the name each argument binds
	// under before the row's index, such as p_id_; it is nil for any other form.
	Names []string

	// Values covers the VALUES row in the SQL Render writes for the query with the same
	// renderer: the generated code keeps the SQL before and after it, which binds no
	// parameter, and writes its rows in its place.
	Values Range

	// Kind is the kind of the renderer's placeholder form, which says what a place takes.
	Kind dialect.PlaceholderKind
}
```

### The conformance suite

```go
package emit_testkit // import "seagull.sh/seagull/sdk/emit/emit_testkit"

type Suite = emittest.Suite
    Suite describes an emitter for Run: how to build it, the dialects it
    generates for, the optional runtime harness, and the checks it is known to
    fail today.
```

The declaration `Suite` aliases, written with the names `sdk/emit/emit_testkit` exports:

```go
type Suite struct {
	// KnownFailures records failing checks and their reasons, keyed by check name or
	// <dialect>/<check>. Listed checks must still fail, and each entry must give a reason.
	KnownFailures map[string]string

	// New returns a fresh emitter for one generation with the dialect whose facts info
	// holds. It must not change info.
	New func(info *dialect.Facts) emit.EmitterPort

	// Harness, when set, adds the runtime lane: the generated code is built with the
	// harness's recording stand-in for the driver, and every method of the runtime cases is
	// called. The runtime checks are skipped without it.
	Harness *Harness

	// Name names the emitter in messages.
	Name string

	// Dialects lists the suite's prebuilt dialects, each with a distinct name. Run adds the
	// martian dialect, an invented engine that shares no convention with a real database,
	// unless one of that name is listed, so every emitter is shown to assume no real
	// engine; a known failure may name it. The suite serialises access to each dialect.
	Dialects []dialect.Dialect

	// Corpora lists golden-style case directories written in one dialect's SQL, whose output
	// the corpus checks examine.
	Corpora []Corpus
}
```

## Refusing a request

A built-in emitter's `Emit` first refuses a request it cannot emit as it is: a nil request or query, a package name that is not a Go identifier, a header that is not one comment line, a helper or error prefix of the wrong shape, or a naming plan that does not fit its queries. A request without a plan is planned first, and refused when planning finds an error. `Emit` then refuses a request its own `Check` finds an error problem with, for a caller that emits without checking. The second refusal's error names each error problem's query, code and message, leaving out a code the problem does not give. Through `Generate`, the generator calls `Check` first and reports its problems as diagnostics, so a query the emitter refuses shows as Q059 or Q060 rather than as an error.

Each built-in `Check` starts with `emit_kit.CheckTypeDepth`, which reports a [Q059](diagnostics.md#q059) error for every parameter, output column and modelled table column whose type nests element, key, field or member types 64 or more levels deep, since every walk an emitter makes through a type stops there. `emit_kit.CheckBatch` takes optional `BindLimit` values, the most variables one statement binds in a placeholder form: a `VALUES` row the emitter repeats within a statement that binds more is a [Q060](diagnostics.md#q060) error, since it could not be sent even alone.

## The style of the generated code

`emit.Style` carries the preset's choices. Two change the shape of every method: `ModernIdioms`, set by the default preset, makes a stream method return `iter.Seq2[Row, error]` and parts every two top-level declarations with a blank line, and `emit_kit.DefaultReservation` then reserves the `iter` import; `WrapErrors` labels every error a method returns with the error prefix and the method's name, through helpers in `error_wrapping.go`, as [labelled errors](generated-go.md#labelled-errors-wraperrors) shows. The built-in emitters write a NUL, a byte order mark or a byte that is not valid UTF-8 in a description or a schema comment as U+FFFD, so a comment never makes generated source invalid.
