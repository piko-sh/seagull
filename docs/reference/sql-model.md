---
title: SQL model
description: "The shared model describes catalogues, SQL types and analysed queries."
section: Reference
subsection: Embedding
order: 1040
---

# SQL model

The shared model describes catalogues, SQL types and analysed queries. Emitters and
schema sources exchange these values without importing internal pipeline packages.
The generator copies a source catalogue before analysis, so a schema source can return
the same value again without generation changing it.

The model is split between two areas of the `sdk` kit, and each name has one home. Its
schema and type half (`Catalogue`, `Schema`, `Table`, `Column`, `TableName`, `SQLType` and
`SQLTypeCategory`, with `NewColumn` and `NewSQLType` to build them) belongs to
`seagull.sh/seagull/sdk/dialect`, where a schema source fills a catalogue and an emitter
reads the catalogue and the SQL types of its queries. Its query and Go-type half
(`AnalysedQuery`, `QueryParameter`, `OutputColumn`, `SQLTemplate`, `GoType`,
`GoTypeResolver`, `TypeSlots` and `GeneratedFile` with their companions) belongs to
`seagull.sh/seagull/sdk/emit`, where emitters read it. The root package names the types
an embedder configures (`CustomFunction`, `GoType`, `TypeOverride`, `TypeSlots` and
`Slot`). A value the model reaches but no facade names, such as a catalogue's views,
functions and enums, is read through the field that holds it.

Seagull follows the element, key, field and member types nested in a SQL type to
`dialect.MaxTypeDepth` (64) levels. The built-in emitters refuse a type that nests that
deep or deeper, through `emit_kit.CheckTypeDepth`, rather than read it in part. The type
arguments of a `go_type` override may nest at most 16 levels.

[Catalogue design](../explanation/schema-catalogue.md) explains ownership and replay;
[the generator API](generator-api.md) describes integration. Test integrations after
upgrading Seagull because this API is evolving.

Source: [`sdk/dialect/facade.go`](../../sdk/dialect/facade.go) and
[`sdk/emit/facade.go`](../../sdk/emit/facade.go).

## Exported package inventory

These declarations are the model's part of the current `go doc -short` output of each
package. Most are aliases of the implementation under `internal/`, which a program never
imports; run `go doc` on a name for its doc comment.

### Schema and types in sdk/dialect

[Package source](../../sdk/dialect) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/dialect)

`go doc -short` folds the type categories into their first, `TypeCategoryInteger`.

```go
const TypeCategoryInteger = sqlmodel.TypeCategoryInteger ...
const MaxTypeDepth = sqlmodel.MaxTypeDepth
type Catalogue = sqlmodel.Catalogue
type Column = sqlmodel.Column
    func NewColumn(name string, sqlType SQLType, nullable bool) Column
type MigrationOrigin = sqlmodel.MigrationOrigin
type SQLType = sqlmodel.SQLType
    func NewSQLType(category SQLTypeCategory, engineName string) SQLType
type SQLTypeCategory = sqlmodel.SQLTypeCategory
type Schema = sqlmodel.Schema
type Table = sqlmodel.Table
type TableName = sqlmodel.TableName
```

### Queries and Go types in sdk/emit

[Package source](../../sdk/emit) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/emit)

`go doc -short` folds the query commands, the segment kinds and the parameter directives
into their first names.

```go
const QueryCommandOne = sqlmodel.QueryCommandOne ...
const SegmentText = sqlmodel.SegmentText ...
const ParameterDirectiveNone = sqlmodel.ParameterDirectiveNone ...
type AllowedColumn = sqlmodel.AllowedColumn
type AnalysedQuery = sqlmodel.AnalysedQuery
type GeneratedFile = sqlmodel.GeneratedFile
type GoType = sqlmodel.GoType
type GoTypeResolver = sqlmodel.GoTypeResolver
type OutputColumn = sqlmodel.OutputColumn
type ParameterDirectiveKind = sqlmodel.ParameterDirectiveKind
type QueryCommand = sqlmodel.QueryCommand
type QueryParameter = sqlmodel.QueryParameter
type SQLSegment = sqlmodel.SQLSegment
type SQLSegmentKind = sqlmodel.SQLSegmentKind
type SQLTemplate = sqlmodel.SQLTemplate
type TypeSlot = sqlmodel.TypeSlot
type TypeSlots = sqlmodel.TypeSlots
```

### In the root package

[Package source](../../types.go) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull)

```go
type CustomFunction = sqlmodel.CustomFunctionConfig
type GoType = sqlmodel.GoType
type Slot = sqlmodel.TypeSlot
type TypeOverride = sqlmodel.TypeOverride
type TypeSlots = sqlmodel.TypeSlots
```
