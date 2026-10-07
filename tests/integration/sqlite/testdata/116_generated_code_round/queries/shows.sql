-- sgl.query(name: InsertShow, command: exec)
INSERT INTO shows (id, data, tags, payload) VALUES (?, ?, ?, CAST(? AS BLOB));

-- sgl.query(name: GetShow, command: one)
SELECT id, data, tags FROM shows WHERE id = ?;

-- sgl.query(name: ListShows, command: many)
SELECT * FROM shows ORDER BY id;

-- sgl.query(name: ExtractPayloads, command: many)
-- sgl.column(doc, go_type: "encoding/json.RawMessage")
SELECT json_extract(payload, '$') AS doc FROM shows ORDER BY id;
