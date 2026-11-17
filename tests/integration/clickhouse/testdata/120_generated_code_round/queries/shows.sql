-- sgl.query(name: ListShows, command: many)
SELECT * FROM shows ORDER BY id;

-- sgl.query(name: ListTitles, command: many)
SELECT s.* FROM shows s ORDER BY s.id;

-- sgl.query(name: InsertDocument, command: exec)
INSERT INTO documents (id, data) VALUES ({id:Int64}, {data:JSON});

-- sgl.query(name: GetDocument, command: one)
SELECT id, data FROM documents WHERE id = {id:Int64};
