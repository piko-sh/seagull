-- sgl.query(name: SchemaVersion, command: one)
PRAGMA user_version;

-- sgl.query(name: Columns, command: many)
PRAGMA table_info(ledger);

-- sgl.query(name: UseWAL, command: one)
PRAGMA journal_mode = WAL;

-- sgl.query(name: EnforceKeys, command: exec)
PRAGMA foreign_keys = ON;
