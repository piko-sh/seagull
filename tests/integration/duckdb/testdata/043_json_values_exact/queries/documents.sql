-- sgl.query(name: ListDocuments, command: many)
SELECT id, body, note, bodies FROM documents ORDER BY id;

-- sgl.query(name: InsertDocument, command: exec)
INSERT INTO documents (id, body, note, amount, span) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(name: FirstTwo, command: many)
SELECT body, note, amount, span FROM documents WHERE id = 1
UNION ALL
SELECT body, note, amount, span FROM documents WHERE id = 2;
