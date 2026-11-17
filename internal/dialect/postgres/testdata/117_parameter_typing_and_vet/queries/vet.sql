-- sgl.query(name: MissingTarget, command: exec)
INSERT INTO authors (id, nickname) VALUES ($1, 'x');

-- sgl.query(name: NestedUnknown, command: many)
SELECT id FROM authors WHERE id IN (SELECT writer FROM books) AND EXISTS (SELECT 1 FROM shelves);

-- sgl.query(name: CTEOutsideFrom, command: many)
WITH found AS (SELECT id FROM books) SELECT id FROM authors WHERE found.id = authors.id;

-- sgl.query(name: UpsertTypo, command: exec)
INSERT INTO authors (id, name) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET nmae = EXCLUDED.name;

-- sgl.query(name: RenameByName, command: exec)
UPDATE authors SET name = $1 WHERE id = $1;
