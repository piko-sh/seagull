-- sgl.query(name: Documents, command: many)
SELECT id, doc, raw, label FROM settings;

-- sgl.query(name: BlobFields, command: many)
SELECT json_extract(blob_doc, '$.a') AS a, json(blob_doc) AS doc, json_type(blob_doc) AS kind FROM settings;

-- sgl.query(name: StoreBinary, command: exec)
INSERT INTO settings (id, doc, label) VALUES (?, jsonb(?), ?);
