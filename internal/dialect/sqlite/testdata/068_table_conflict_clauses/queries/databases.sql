-- sgl.query(name: ListDatabases, command: many)
SELECT * FROM databases WHERE account_id = ?;

-- sgl.query(name: ListEngines, command: many)
SELECT engine, version FROM engines;
