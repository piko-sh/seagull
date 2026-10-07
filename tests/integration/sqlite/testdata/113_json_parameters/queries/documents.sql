-- sgl.query(RecordDocument, exec)
INSERT INTO documents (id, data) VALUES (?, ?);

-- sgl.query(RecordDocuments, batch)
INSERT INTO documents (id, data) VALUES (?, ?);

-- sgl.query(ReadDocument, one)
SELECT id, data FROM documents WHERE id = ?;

-- sgl.query(ListStorage, many)
SELECT id, typeof(data) AS storage, json_valid(data) AS valid, CAST(json_extract(data, '$.a') AS TEXT) AS extracted,
    CAST(data AS TEXT) AS stored
FROM documents ORDER BY id;

-- sgl.query(FindByLiteral, many)
SELECT id FROM documents WHERE data = '{"b": [1, 2.50], "a": "<é & 😀>"}' ORDER BY id;

-- sgl.query(FindByParameter, many)
SELECT id FROM documents WHERE data = ? ORDER BY id;
