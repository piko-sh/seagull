---
title: Migration and seed files
description: Migration files describe schema evolution. Generation replays their DDL
  into a catalogue; the migration runtime executes their statements against a database.
section: Reference
subsection: Core
order: 260
---

# Migration and seed files

Migration files describe schema evolution. Generation replays their DDL into a catalogue; the migration runtime executes their statements against a database.

```
db/
  migrations/
    001_initial.up.sql
    001_initial.down.sql
    002_add_comments.up.sql
    002_add_comments.down.sql
```

Every file follows `{version}_{name}.{up|down}.sql`: a numeric version, an underscore, a
non-empty name, then `.up.sql` or `.down.sql`. Versions are compared as integers, so use
zero-padded numbers (`001_initial`) or timestamps (`20260115_initial`) and keep to one
scheme. Generation reads the files exactly as the runtime does: it replays the up files in
version order, one file at a time, and ignores every other file. Each `.sql` file that does
not match the scheme, such as `schema.sql` or `001_initial.sql`, is reported as ignored
([Q048](diagnostics.md#q048)). Two files with the same version and direction are an error
([Q053](diagnostics.md#q053)): the runtime refuses to run either, so generation replays
only the first.

Migrations in subdirectories are not read, so a directory-per-migration layout such as
`01_init/up.sql` is reported too (Q048), as an error when nothing else is left to apply.
An up file runs whole, so the down half of another tool's single-file layout, after a
marker line such as goose's `-- +goose Down`, sql-migrate's `-- +migrate Down`, dbmate's
`-- migrate:down` or tern's `---- create above / drop below ----`, would undo the migration
as soon as it ran: generation reports the marker as an error (Q048), the migration runner
refuses to apply such a file with the same message before it runs anything, and the
statements after the marker belong in the version's `.down.sql` file.

A down file is optional; without one, rolling that version back fails with
`migrate.NoDownMigrationError`.

Named pipes, sockets and devices in the directory are passed over, even when their names
end in `.sql`, so reading the directory never waits on another process. A symbolic link is
read as the file it points to, which must be a regular file.

`sgl migrate new add_comments` creates the next pair for you.

## Statement boundaries and transaction directives

The runner splits each file into statements exactly as the dialect does when `sgl generate`
and `sgl vet` read it: under the engine's lexical rules, with the splitter the generator
uses.
Quoted identifiers such as `"a;b"`, MySQL's double-quoted strings and `#` comments,
PostgreSQL's `E'...'` strings, nested comments and dollar-quoted bodies, and procedural
`BEGIN ... END` bodies all stay within their statement, and a column named `begin` is just a
column. MySQL's `DELIMITER` lines change the terminator, as in the `mysql` client, and are
not sent to the server:

```sql
DELIMITER //
CREATE FUNCTION double_it(x INT) RETURNS INT DETERMINISTIC
BEGIN
    RETURN x * 2;
END //
DELIMITER ;
```

On PostgreSQL, TimescaleDB and CockroachDB a line that starts with a backslash where a
statement could begin is a psql meta-command, such as the `\restrict` and `\unrestrict`
lines `pg_dump` 17.6 and later write around a dump: psql runs it itself, so it belongs to no
statement, and a dumped schema replays and runs without it.

`Backend.Split(content)` returns the statements the runner would run for a file.

A statement whose semicolon is missing runs on into the next one, which the engine rejects;
generation reports it as [Q010](diagnostics.md#q010) at the first word of the next
statement (`a semicolon is missing before CREATE`) rather than reading the second statement
as part of the first and losing it. This holds however long the first statement is: one
longer than the token budget (`max_tokens_per_statement`), such as a large seed `INSERT`,
is read to its end for it.

## How the DDL replays

Generation models supported DDL in the catalogue without running it against a
database:

- `IF NOT EXISTS` and `IF EXISTS` behave as they do on the server. A `CREATE ... IF NOT
  EXISTS` of a table, view, type, sequence or function that exists changes nothing, and an
  `ADD COLUMN IF NOT EXISTS` or `ADD VALUE IF NOT EXISTS` skips what is already there; an
  `ALTER ... IF EXISTS` of a missing table, view or type, or a column action written `IF
  EXISTS` on a missing column, changes nothing. Without the clause, creating what exists,
  or renaming or moving onto a name that is taken, is reported as
  [Q067](diagnostics.md#q067), and altering, renaming or dropping what is missing as
  [Q068](diagnostics.md#q068), as the engine would refuse them.
- `FIRST`, `AFTER` and `BEFORE` place the column an `ADD COLUMN`, `MODIFY` or `CHANGE`
  adds or moves (MySQL, MariaDB, ClickHouse) and the value an `ALTER TYPE ... ADD VALUE`
  adds (PostgreSQL), so `SELECT *` scans in table order and enum constants keep the
  type's order.
- Renames and moves to another schema follow the object: `ALTER TABLE`, `ALTER VIEW`
  and `ALTER MATERIALIZED VIEW ... RENAME TO` or `SET SCHEMA`, `RENAME TABLE`, and `ALTER
  TYPE` or `ALTER DOMAIN ... RENAME TO` or `SET SCHEMA`, whose columns follow the type.
- A view defined with `SELECT *` or `t.*` holds the columns the star stands for, under
  any column names the view declares. On SQLite and DuckDB, which read a view's
  definition anew for every query, the view also takes the columns a later migration
  adds to its tables; elsewhere its columns are fixed when it is created.
- `CREATE TABLE ... PARTITION OF` (PostgreSQL, TimescaleDB) makes a table queries can
  read, holding its parent's columns; see [partitions](schema-api.md#partitions).
- On PostgreSQL, CockroachDB and TimescaleDB, `SET search_path` and `SET SCHEMA` move where
  the later statements create and look up unqualified objects, in the same file and the
  later ones, as the runner's one session does
  ([sessions and the search path](migration-runtime.md#sessions-and-the-search-path)).
  `SET LOCAL` lasts only for its migration, and `RESET search_path`, `RESET ALL` and `SET
  search_path TO DEFAULT` go back to the path the migrations start with,
  `dialect_options.search_path` (`public` by default). The queries are analysed with that
  starting path, as the application's sessions run them. MySQL's `USE` is not followed, so
  name the database instead (`CREATE TABLE other.widgets ...`).
- A primary key makes its columns `NOT NULL` where the engine does: always on
  PostgreSQL, MySQL, MariaDB and DuckDB, whether the key is declared on a column, as a
  table constraint or added later; on SQLite only for the rowid alias, a key declared
  `NOT NULL` and the key of a `WITHOUT ROWID` or `STRICT` table.
- PostgreSQL `INHERITS` puts the parents' columns first, merges a column of the same
  name declared more than once into one, and passes a parent's later `ADD`, `DROP`,
  `RENAME` and type or nullability change on to the tables that inherit from it (not
  for `ALTER TABLE ONLY`); dropping a parent drops them too. `LIKE`, and MySQL's `CREATE
  TABLE ... LIKE`, copy a table's columns as they stand, where the element is written.

## Transactions

Each migration runs in a transaction with its history row where the database supports
transactional DDL: PostgreSQL, TimescaleDB, SQLite and DuckDB. The runner recognises the
statements an engine refuses inside a transaction, and runs a file holding one without
one: `CREATE INDEX CONCURRENTLY`, `REINDEX CONCURRENTLY` and `VACUUM` on PostgreSQL, a
leading `VACUUM` on SQLite and DuckDB, and on `migrate_backend_timescaledb.New()` a continuous
aggregate created with data or refreshed with `refresh_continuous_aggregate`. A mention
inside a string, a quoted identifier or a comment does not count. For anything else, the
directive at the top of a file turns the transaction off:

```sql
-- sgl.migration(no_transaction: true)
CREATE INDEX CONCURRENTLY tasks_title_idx ON tasks (title);
```

Write the directive exactly. One the runner cannot read for certain, such as
`no_transacton: true`, a value other than `true` or `false`, a call left unclosed or a
misspelt `sgl.migraton`, makes the runner refuse the migration with an error naming the
file, the line and the problem, before anything runs, since
running it inside or outside a transaction against what it asked cannot be undone.
`sgl generate` and `sgl vet` report the directive where it is written.

MySQL, MariaDB, CockroachDB and ClickHouse run no migration in a transaction, so on those
backends every file behaves as if it carried the directive; see
[MySQL and MariaDB](migration-runtime.md#mysql-and-mariadb) and [CockroachDB](migration-runtime.md#cockroachdb).

A migration that runs without a transaction is not atomic, so keep a statement that needs
one in a file of its own. A TimescaleDB continuous aggregate can also be created
`WITH NO DATA`, which runs in a transaction, and filled by a refresh policy:

```sql
CREATE MATERIALIZED VIEW hourly_temps
WITH (timescaledb.continuous)
AS SELECT time_bucket('1 hour', ts) AS bucket, avg(temperature) AS avg_temp
FROM readings
GROUP BY bucket
WITH NO DATA;
```

Data-only seeds and their history rows share a transaction on every built-in backend
except ClickHouse. Schema-changing seed SQL can break that atomicity on engines whose
DDL commits implicitly.

## Seed files

Seeds hold reference data and fixtures. They live in their own directory, use the same
numbered names (`001_countries.sql`). `Apply` skips versions already recorded as
successfully applied:

```sh
sgl seed apply
sgl seed status
sgl seed reseed     # clear the seed history and apply every seed again
```

From Go, `migrate.NewSeeder(database, migrate_backend_postgres.New(), seeds, "db/seeds")` returns a
`*migrate.Seeder` with the same three operations: `Apply`, `Status` and `Reseed`. Write seeds to be idempotent
(`INSERT ... ON CONFLICT DO NOTHING`), since `reseed` runs them again and a failure
without a transaction can leave unrecorded effects. See [seed operations](../how-to/seeds-namespaces-hooks.md).
