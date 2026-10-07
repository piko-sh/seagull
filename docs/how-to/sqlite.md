---
title: Use SQLite databases and extensions
description: Use a SQLite driver registered with database/sql and a file DSN for persistent
  application data. For tests, prefer a temporary file or deliberately managed in-memory
  connections.
section: How-to guides
subsection: Dialects
order: 700
---

# Use SQLite databases and extensions

Use SQLite’s FTS5 virtual table to search text while keeping the result typed. The working example also supplies a spatial index and JSON queries.

The steps below use `sgl` and `sgl.yaml`. A program that generates or migrates from Go imports the SQLite providers of the `sdk` kit instead:

| Role | Package | Constructor |
|---|---|---|
| Dialect | `seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite` | `dialect_engine_sqlite.New()` |
| Emitter | `seagull.sh/seagull/sdk/emit/emit_driver_databasesql` | `emit_driver_databasesql.New()`, the default |
| Migrations | `seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite` | `migrate_backend_sqlite.New()`, for `migrate.NewMigrator` |
| Live schema | `seagull.sh/seagull/sdk/schema/schema_source_sqlite` | `schema_source_sqlite.New(database, engine)`, for `seagull.WithSchemaSource` |

## Create and populate the full-text index

Use `dialect: sqlite` and the database/sql emitter. Add the source table and virtual indexes in a migration; the insert trigger populates the indexes when a place is added. `tag_count` is a generated column, which SQLite computes from `tags` and refuses writes to, and `content=places` keeps the text in `places` itself, so the full-text index holds only the words, keyed by `places.id`:

```sql
CREATE TABLE places (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL,
    tags        TEXT    NOT NULL DEFAULT '[]',
    west        REAL    NOT NULL,
    east        REAL    NOT NULL,
    south       REAL    NOT NULL,
    north       REAL    NOT NULL,
    tag_count   INTEGER NOT NULL GENERATED ALWAYS AS (json_array_length(tags)) VIRTUAL
);

CREATE VIRTUAL TABLE places_search USING fts5(name, description, content=places, content_rowid=id);

CREATE VIRTUAL TABLE places_area USING rtree(id, min_x, max_x, min_y, max_y);

CREATE TRIGGER places_indexed AFTER INSERT ON places BEGIN
    INSERT INTO places_search (rowid, name, description) VALUES (new.id, new.name, new.description);
    INSERT INTO places_area (id, min_x, max_x, min_y, max_y) VALUES (new.id, new.west, new.east, new.south, new.north);
END;
```

Add the full-text query to `db/queries/places.sql`:

```sql
-- sgl.query(SearchDescriptions, many)
SELECT p.name, snippet(places_search, 1, '[', ']', '...', 6) AS excerpt
FROM places_search
JOIN places AS p ON p.id = places_search.rowid
WHERE places_search.description MATCH :terms
ORDER BY rank;
```

If your application updates or deletes places, add matching index-maintenance triggers too; this example covers inserts.

## Pass the search expression as a parameter

Run `sgl generate`, apply the migration, and construct `store.New(database)`. Pass the FTS expression through the generated argument:

```go
	found, err := queries.SearchDescriptions(ctx, "gull*")
	if err != nil {
		return fmt.Errorf("searching descriptions: %w", err)
	}
	fmt.Fprintln(output, `descriptions matching "gull*":`)
	for _, place := range found {
		excerpt := ""
		if place.Excerpt != nil {
			excerpt = *place.Excerpt
		}
		fmt.Fprintf(output, "  %-12s %s\n", place.Name, excerpt)
	}
```

The expression `gull*` searches for words with that prefix. The SQL controls ranking and the highlighted snippet; the caller handles nullable rank values as the generated API requires.

## Check FTS5 against the selected SQLite driver

From the Seagull repository root, run the SQLite extension example:

```sh
cd examples/sqlite/040_sqlite_extensions
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/040_sqlite_extensions/example.go) for the exact Go API and its [test](../../examples/sqlite/040_sqlite_extensions/main_test.go) for assertions and failure handling.

The complete example produces:

```text
added 1 The Old Pier     tag_count=2
added 2 Harbour Wall     tag_count=2
added 3 Chip Shop        tag_count=1
added 4 Lifeboat Station tag_count=3
descriptions matching "gull*":
  The Old Pier Wooden pier where the herring [gulls]...
  Chip Shop    ...away; guard them from the [gulls]
at (12, 4): The Old Pier
at (70, 20): Lifeboat Station
tagged walk: Harbour Wall     tag_count=2
tagged walk: Lifeboat Station tag_count=3
```

## Check extension availability

Choose a SQLite build with the extensions your SQL uses. Modernc enables FTS5; mattn’s driver needs its FTS5 build tag. SQLite’s declared type names and affinity rules also affect generated Go types; use [type mapping](inspect-type-mapping.md) before introducing overrides.

See the [dialects/sqlite reference](../reference/dialects/sqlite.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
