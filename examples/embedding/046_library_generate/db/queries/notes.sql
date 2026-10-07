-- sgl.query(AddNote, exec)
INSERT INTO notes (body, settings) VALUES (?, ?);

-- sgl.query(ListNotes, many)
SELECT id, body, settings FROM notes ORDER BY id;
