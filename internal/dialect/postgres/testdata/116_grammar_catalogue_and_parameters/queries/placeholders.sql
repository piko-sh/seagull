-- sgl.query(name: ByAt, command: many)
SELECT id FROM places WHERE label = @label;

-- sgl.query(name: ByQuestion, command: exec)
INSERT INTO marks (id, row) VALUES (?, ?);

-- sgl.query(name: OptionalLabel, command: many)
SELECT id FROM places WHERE (:label IS NULL OR label = :label);

-- sgl.query(name: CastLabel, command: many)
SELECT id FROM places WHERE (:label::text IS NULL OR label = :label);
