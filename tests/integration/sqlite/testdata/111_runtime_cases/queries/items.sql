-- sgl.query(name: ItemsIn, command: many)
-- ?1 as sgl.param(ids, kind: slice)
SELECT id FROM items WHERE id IN (?1) ORDER BY id;

-- sgl.query(name: ItemsNotIn, command: many)
-- ?1 as sgl.param(ids, kind: slice)
SELECT id FROM items WHERE id NOT IN (?1) ORDER BY id;

-- sgl.query(name: GetItem, command: one)
SELECT id, kind, score FROM items WHERE id = ?;

-- sgl.query(name: UpsertItems, command: batch)
INSERT INTO items (id, kind, score) VALUES (?, lower(?), ? + 1)
ON CONFLICT (id) DO UPDATE SET kind = excluded.kind, score = excluded.score;

-- sgl.query(name: CopyEntries, command: copyfrom)
INSERT INTO main.entries (actor, action, id) VALUES (?, ?, ?);

-- sgl.query(name: ListEntries, command: many)
SELECT id, action, actor FROM entries ORDER BY id;
