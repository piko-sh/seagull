-- sgl.query(name: CreateNote, command: one)
INSERT INTO notes (title, body) VALUES (?, ?) RETURNING id, title, body;

-- sgl.query(name: GetNote, command: one)
SELECT id, title, body FROM notes WHERE id = ?;

-- sgl.query(name: ListNotes, command: many)
SELECT id, title, body FROM notes ORDER BY id;

-- sgl.query(name: UpdateNoteTitle, command: execrows)
UPDATE notes SET title = ? WHERE id = ?;

-- sgl.query(name: DeleteNote, command: exec)
DELETE FROM notes WHERE id = ?;
