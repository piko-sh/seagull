-- sgl.query(RecordDocument, exec)
INSERT INTO documents (id, data) VALUES ($1, $2);

-- sgl.query(RecordDocuments, batch)
INSERT INTO documents (id, data) VALUES ($1, $2);

-- sgl.query(ReadDocument, one)
SELECT id, data FROM documents WHERE id = $1;

-- sgl.query(ListStorage, many)
SELECT id, typeof(data) AS storage, json_type(data) AS kind, data->>'$.a' AS extracted, CAST(data AS VARCHAR) AS stored
FROM documents ORDER BY id;

-- sgl.query(FindByLiteral, many)
SELECT id FROM documents WHERE data = '{"b": [1, 2.50], "a": "<é & 😀>"}' ORDER BY id;

-- sgl.query(FindByParameter, many)
SELECT id FROM documents WHERE data = $1 ORDER BY id;

-- sgl.query(RecordComposite, exec)
INSERT INTO composites (id, documents, pair) VALUES ($1, $2, $3);

-- sgl.query(ReadComposite, one)
SELECT id, documents, pair FROM composites WHERE id = $1;

-- sgl.query(ListCompositeStorage, many)
SELECT id, typeof(documents[1]) AS storage, documents[1]->>'$.a' AS extracted, CAST(documents[1] AS VARCHAR) AS stored,
    CAST(struct_extract(pair, 'doc') AS VARCHAR) AS field_stored
FROM composites ORDER BY id;

-- sgl.query(FindCompositeByLiteral, many)
SELECT id FROM composites WHERE documents[1] = '{"b": [1, 2.50], "a": "<é & 😀>"}' AND struct_extract(pair, 'doc') = '{"b": [1, 2.50], "a": "<é & 😀>"}' ORDER BY id;
