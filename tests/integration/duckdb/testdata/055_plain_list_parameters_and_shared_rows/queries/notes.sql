-- sgl.query(AddNote, exec)
INSERT INTO notes (id, tags, scores, limits) VALUES ($1, $2, $3, $4);

-- sgl.query(AddNullElements, exec)
INSERT INTO notes (id, tags, scores, moods, limits)
VALUES ($1, ['a', NULL], [1, NULL], ['calm'::mood, NULL], MAP {'x': 1, 'y': NULL});

-- sgl.query(GetNote, one, model: notes)
SELECT limits, moods, scores, tags, id FROM notes WHERE id = $1;

-- sgl.query(ListNotes, many)
SELECT id, tags, scores, moods, limits FROM notes ORDER BY id;
