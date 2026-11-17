-- sgl.query(StreamDocuments, stream)
SELECT id, data FROM documents WHERE id < {below:Int32} ORDER BY id;

-- sgl.query(ReadDocumentUnion, many)
SELECT id, data FROM documents WHERE id = {id:Int32}
UNION ALL
SELECT id + 100, data FROM documents WHERE id = {other:Int32}
ORDER BY id;

-- sgl.query(ListOwnerDocuments, many)
-- sgl.embed(documents, from: d)
SELECT o.id, o.name, d.id, d.data
FROM owners o
INNER JOIN documents d ON d.id = o.id
ORDER BY o.id;

-- sgl.query(SearchDocuments, many, dynamic: runtime)
SELECT id, data FROM documents;

-- sgl.query(ReadOptionalDocument, one)
SELECT id, data FROM optional_documents WHERE id = {id:Int32};

-- sgl.query(ReadNested, one)
SELECT id, optional_keyed, listed, optional_list, pairs, unnamed, listed_unnamed, keyed_unnamed FROM nested_documents WHERE id = {id:Int32};

-- sgl.query(ReadCompositeUnion, many)
SELECT id, documents, pair FROM composites WHERE id = {id:Int32}
UNION ALL
SELECT id + 100, documents, pair FROM composites WHERE id = {other:Int32}
ORDER BY id;

-- sgl.query(ReadRichDocument, one)
SELECT id, data, [data] AS listed, toJSONString(data) AS stored FROM rich_documents WHERE id = {id:Int32};

-- sgl.query(ReadComposite, one)
SELECT id, documents, pair FROM composites WHERE id = {id:Int32};
