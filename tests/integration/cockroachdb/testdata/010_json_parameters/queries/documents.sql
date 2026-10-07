-- sgl.query(RecordDocument, exec)
INSERT INTO documents (id, data, document) VALUES ($1, $2, $3);

-- sgl.query(RecordDocuments, batch)
INSERT INTO documents (id, data, document) VALUES ($1, $2, $3);

-- sgl.query(ReadDocument, one)
SELECT id, data, document FROM documents WHERE id = $1;

-- sgl.query(ListStorage, many)
SELECT id, json_typeof(data) AS storage, jsonb_typeof(document) AS binary_storage, data->>'a' AS extracted,
    document->>'a' AS binary_extracted, data::STRING AS stored, document::text AS binary_stored
FROM documents ORDER BY id;

-- sgl.query(FindByLiteral, many)
SELECT id FROM documents WHERE data = '{"b": [1, 2.50], "a": "<é & 😀>"}'::JSONB ORDER BY id;

-- sgl.query(FindByBinaryLiteral, many)
SELECT id FROM documents WHERE document = '{"b": [1, 2.50], "a": "<é & 😀>"}'::jsonb ORDER BY id;

-- sgl.query(FindByParameter, many)
SELECT id FROM documents WHERE document = $1 ORDER BY id;
