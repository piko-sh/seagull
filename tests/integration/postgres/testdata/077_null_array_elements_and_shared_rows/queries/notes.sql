-- sgl.query(AddNote, exec)
INSERT INTO notes (id, tags, scores, moods) VALUES ($1, $2, $3, $4);

-- sgl.query(SetTags, exec)
UPDATE notes SET tags = $1 WHERE id = $2;

-- sgl.query(GetNote, one, model: notes)
SELECT moods, scores, tags, id FROM notes WHERE id = $1;

-- sgl.query(ListNotes, many)
SELECT id, tags, scores, moods FROM notes ORDER BY id;

-- sgl.query(NotesByID, many)
SELECT id FROM notes WHERE id = ANY($1) ORDER BY id;

-- sgl.query(NotesWithoutTags, many)
-- $1 as sgl.param(tags, non_null_elements: true)
SELECT id FROM notes WHERE NOT (tags && $1::text[]) ORDER BY id;

-- sgl.query(StoredNotes, many)
SELECT id, array_to_string(tags, ',', 'NULL') AS tags, array_to_string(scores, ',', 'NULL') AS scores,
  array_to_string(moods, ',', 'NULL') AS moods
FROM notes ORDER BY id;
