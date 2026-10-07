-- sgl.query(name: InsertNote, command: exec)
INSERT INTO notes (title, body) VALUES ($1, $2);

-- sgl.query(name: GetNote, command: one)
SELECT id, title, body FROM notes WHERE id = $1;

-- sgl.query(name: ListNotes, command: many)
SELECT id, title, body FROM notes ORDER BY id;
