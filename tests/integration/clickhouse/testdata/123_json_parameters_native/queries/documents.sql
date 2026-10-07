-- sgl.query(RecordDocument, exec)
INSERT INTO documents (id, data) VALUES ({id:Int32}, {data:JSON});

-- sgl.query(RecordDocuments, batch)
INSERT INTO documents (id, data) VALUES ({id:Int32}, {data:JSON});

-- sgl.query(CopyDocuments, copyfrom)
INSERT INTO documents (id, data) VALUES ({id:Int32}, {data:JSON});

-- sgl.query(RecordDocumentRows, batch)
INSERT INTO documents (id, data) VALUES ({id:Int32} + 0, {data:JSON});

-- sgl.query(ListStorage, many)
SELECT id, toTypeName(data) AS storage, JSONExtractString(toJSONString(data), 'a') AS extracted, toJSONString(data) AS stored
FROM documents ORDER BY id;

-- sgl.query(FindByLiteral, many)
SELECT id FROM documents WHERE toJSONString(data) = toJSONString(CAST('{"b": [1, 2.50], "a": "<é & 😀>"}' AS JSON)) ORDER BY id;

-- sgl.query(FindByParameter, many)
SELECT id FROM documents WHERE toJSONString(data) = toJSONString({document:JSON}) ORDER BY id;

-- sgl.query(RecordComposite, exec)
INSERT INTO composites (id, documents, pair) VALUES ({id:Int32}, {documents:Array(JSON)}, {pair:Tuple(doc JSON, n Int64)});

-- sgl.query(ReadComposite, one)
SELECT id, documents, pair FROM composites WHERE id = {id:Int32};

-- sgl.query(ListCompositeStorage, many)
SELECT id, toJSONString(documents[1]) AS stored, toJSONString(tupleElement(pair, 'doc')) AS field_stored
FROM composites ORDER BY id;

-- sgl.query(FindCompositeByLiteral, many)
SELECT id FROM composites WHERE toJSONString(documents[1]) = toJSONString(CAST('{"b": [1, 2.50], "a": "<é & 😀>"}' AS JSON)) ORDER BY id;
