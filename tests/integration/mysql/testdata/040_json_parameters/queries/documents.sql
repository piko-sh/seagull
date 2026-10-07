-- sgl.query(RecordDocument, exec)
INSERT INTO documents (id, data) VALUES (?, ?);

-- sgl.query(RecordDocuments, batch)
INSERT INTO documents (id, data) VALUES (?, ?);

-- sgl.query(ReadDocument, one)
SELECT id, data FROM documents WHERE id = ?;

-- sgl.query(ListStorage, many)
SELECT id, JSON_TYPE(data) AS storage, JSON_UNQUOTE(JSON_EXTRACT(data, '$.a')) AS extracted, CAST(data AS CHAR) AS stored_text
FROM documents ORDER BY id;

-- sgl.query(FindByLiteral, many)
SELECT id FROM documents WHERE data = CAST('{"b": [1, 2.50], "a": "<é & 😀>"}' AS JSON) ORDER BY id;

-- sgl.query(FindByParameter, many)
SELECT id FROM documents WHERE data = ? ORDER BY id;
