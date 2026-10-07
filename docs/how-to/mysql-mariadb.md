---
title: Use MySQL and MariaDB
description: Select the exact dialect, then use its parameters, writes and DDL in
  migrations and queries. For functions with procedural bodies, preserve the documented
  DELIMITER handling.
section: How-to guides
subsection: Dialects
order: 690
---

# Use MySQL and MariaDB

Use the exact `mysql` or `mariadb` dialect and the database/sql emitter. This example registers a boat once with `INSERT IGNORE` and checks whether a later registration was skipped.

## Configure the connection and duplicate handling

Set `dialect: mysql` (or `mariadb`) and `go.driver: database/sql` in your database configuration. Open go-sql-driver/mysql with `parseTime=true`, for example `app:secret@tcp(localhost:3306)/shop?parseTime=true`, so temporal columns can scan into `time.Time`.

The [migration](../../examples/mysql/039_mysql_dialect_features/db/migrations/0001_harbour.up.sql) makes the boat registration unique. Add the insert as `execrows` so the caller can distinguish insertion from skipping:

```sql
-- sgl.query(RegisterBoat, execrows)
INSERT IGNORE INTO boats (registration, name, kit) VALUES (?, ?, ?);
```

## Check whether the registration was inserted

Run `sgl generate`, construct `store.New(database)`, and inspect the affected-row count. Use the [example caller](../../examples/mysql/039_mysql_dialect_features/example.go) for the surrounding registration loop and error handling:

```go
	for _, boat := range boats {
		registered, err := queries.RegisterBoat(ctx, boat)
		if err != nil {
			return fmt.Errorf("registering %s: %w", boat.Name, err)
		}
		if registered == 0 {
			fmt.Fprintf(output, "INSERT IGNORE  %s is already registered, so %s was ignored\n", boat.Registration, boat.Name)
			continue
		}
		fmt.Fprintf(output, "INSERT IGNORE  registered %s as %s with kit %q\n", boat.Name, boat.Registration, boat.Kit)
	}
```

Use `ON DUPLICATE KEY UPDATE` when an existing row should change instead. MariaDB’s `RETURNING` support does not extend to MySQL; choose the command for your exact write form.

## Check the MySQL write behaviour

From the Seagull repository root, run the dialect example:

```sh
cd examples/mysql/039_mysql_dialect_features
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
INSERT IGNORE  registered Kittiwake as PZ1001 with kit "radar,radio,radar"
INSERT IGNORE  registered Salty Chip as PZ2002 with kit "liferaft"
INSERT IGNORE  PZ1001 is already registered, so Kittiwake II was ignored
INSERT IGNORE  registered Gull Wing as PZ3003 with kit "radio,sonar"
REPLACE INTO   berth 1 for PZ1001, rows affected: 1
REPLACE INTO   berth 2 for PZ2002, rows affected: 1
REPLACE INTO   berth 1 for PZ2002, rows affected: 3
  berth 1 (uint8): Salty Chip
FIND_IN_SET    Kittiwake has a radio among "radio,radar"
FIND_IN_SET    Gull Wing has a radio among "radio"
GROUP_CONCAT   PZ1001 Kittiwake  crossings 2 (uint32), crew: Morwenna the skipper, Jago the deckhand
GROUP_CONCAT   PZ2002 Salty Chip crossings 0 (uint32), crew: no crew
GROUP_CONCAT   PZ3003 Gull Wing  crossings 1 (uint32), crew: Kerensa the skipper
```

## Match SQL mode and transaction semantics

In the default SQL mode, double quotes delimit strings and `||` is logical OR. Use `CONCAT` for concatenation. If the server enables `ANSI_QUOTES`, set `dialect_options.ansi_quotes: true` so generation and migration splitting agree with it.

MySQL and MariaDB DDL may commit implicitly. Keep migration recovery separate from transaction rollback, and preserve the dialect’s `DELIMITER` handling for procedural bodies.

See the [dialects/mysql reference](../reference/dialects/mysql.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
