-- sgl.query(name: SchemaVersion, command: one)
PRAGMA user_version;

-- sgl.query(name: LedgerColumns, command: many)
PRAGMA table_info(ledger);

-- sgl.query(name: SetVersion, command: exec)
PRAGMA user_version = 9;
