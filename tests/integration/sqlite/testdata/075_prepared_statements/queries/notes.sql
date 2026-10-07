-- sgl.query(name: InsertNote, command: exec)
INSERT INTO notes (id, title, body) VALUES (?, ?, ?);

-- sgl.query(name: GetNote, command: one)
SELECT id, title, body FROM notes WHERE id = ?;

-- sgl.query(name: ListNotes, command: many)
SELECT id, title, body FROM notes ORDER BY id;
