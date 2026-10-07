-- sgl.query(name: InsertShow, command: exec)
INSERT INTO shows (id, data, tags, colours) VALUES ($1, $2, $3, $4);

-- sgl.query(name: GetShow, command: one)
SELECT id, data, tags FROM shows WHERE id = $1;

-- sgl.query(name: ListShows, command: many)
SELECT * FROM shows ORDER BY id;

-- sgl.query(name: ShowsInColours, command: many)
-- :wanted as sgl.param(optional: true)
SELECT id FROM shows WHERE (:wanted::colour[] IS NULL OR colours && :wanted::colour[]) ORDER BY id;
