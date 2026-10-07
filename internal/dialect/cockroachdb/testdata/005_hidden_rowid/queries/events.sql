-- sgl.query(name: ListEvents, command: many)
SELECT * FROM events;

-- sgl.query(name: ListEventIDs, command: many)
SELECT rowid, name FROM events WHERE rowid > $1;

-- sgl.query(name: AddEvent, command: exec)
INSERT INTO events (name) VALUES ($1);

-- sgl.query(name: ListAccounts, command: many)
SELECT * FROM accounts;
