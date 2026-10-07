-- sgl.query(GetNote, one)
SELECT id, title, body FROM notes WHERE id = ?;

-- sgl.query(ListNotesWithTags, many)
SELECT notes.id, notes.title, tags.name AS tag
FROM notes
LEFT JOIN tags ON tags.note_id = notes.id
WHERE notes.archived = 0
ORDER BY notes.id, tags.name;

-- sgl.query(ArchiveNote, execrows)
UPDATE notes SET archived = 1 WHERE id = ?;

-- sgl.query(ArchiveEverything, execrows)
UPDATE notes SET archived = 1;
