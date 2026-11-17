-- sgl.query(name: InsertShow, command: exec)
INSERT INTO shows (id, data, tags) VALUES (?, ?, ?);

-- sgl.query(name: GetShow, command: one)
SELECT id, data, tags FROM shows WHERE id = ?;

-- sgl.query(name: ListShows, command: many)
SELECT * FROM shows ORDER BY id;
