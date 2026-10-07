-- sgl.query(name: Documents, command: many)
SELECT id, doc, label FROM settings ORDER BY id;

-- sgl.query(name: BlobFields, command: many)
SELECT id, json(blob_doc) AS doc, json_extract(blob_doc, '$.a') AS a FROM settings ORDER BY id;

-- sgl.query(name: StoreBinary, command: exec)
INSERT INTO settings (id, doc, label) VALUES (?, jsonb(?), ?);

-- sgl.query(name: ReadBinary, command: one)
SELECT json(doc) AS doc FROM settings WHERE id = ?;
