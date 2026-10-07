-- sgl.query(RecordDocument, exec)
INSERT INTO documents (id, data, document) VALUES ($1, $2, $3);

-- sgl.query(RecordDocuments, batch)
INSERT INTO documents (id, data, document) VALUES ($1, $2, $3);

-- sgl.query(ReadDocument, one)
SELECT id, data, document FROM documents WHERE id = $1;

-- sgl.query(ListStorage, many)
SELECT id, json_typeof(data) AS storage, jsonb_typeof(document) AS binary_storage, data->>'a' AS extracted,
    document->>'a' AS binary_extracted, data::text AS stored, document::text AS binary_stored
FROM documents ORDER BY id;

-- sgl.query(FindByLiteral, many)
SELECT id FROM documents WHERE data::text = '{"b": [1, 2.50], "a": "<é & 😀>"}' ORDER BY id;

-- sgl.query(FindByBinaryLiteral, many)
SELECT id FROM documents WHERE document = '{"b": [1, 2.50], "a": "<é & 😀>"}'::jsonb ORDER BY id;

-- sgl.query(FindByParameter, many)
SELECT id FROM documents WHERE document = $1 ORDER BY id;

-- sgl.query(RecordComposite, exec)
INSERT INTO composites (id, documents, texts) VALUES ($1, $2, $3);

-- sgl.query(ReadComposite, one)
SELECT id, documents, texts FROM composites WHERE id = $1;

-- sgl.query(ListCompositeStorage, many)
SELECT id, jsonb_typeof(documents[1]) AS storage, documents[1]->>'a' AS extracted, documents[1]::text AS stored,
    texts[1]::text AS text_stored
FROM composites ORDER BY id;

-- sgl.query(FindCompositeByLiteral, many)
SELECT id FROM composites WHERE documents[1] = '{"b": [1, 2.50], "a": "<é & 😀>"}'::jsonb AND texts[1]::text = '{"b": [1, 2.50], "a": "<é & 😀>"}' ORDER BY id;
