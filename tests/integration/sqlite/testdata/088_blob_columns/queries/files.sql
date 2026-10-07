-- sgl.query(name: InsertFile, command: exec)
INSERT INTO files (id, name, content, size) VALUES (?, ?, ?, ?);

-- sgl.query(name: GetFile, command: one)
SELECT id, name, content, size FROM files WHERE id = ?;

-- sgl.query(name: ListFileNames, command: many)
SELECT id, name, size FROM files ORDER BY id;
