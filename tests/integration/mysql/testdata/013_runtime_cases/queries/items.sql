-- sgl.query(name: ItemsIn, command: many)
-- ?1 as sgl.param(ids, kind: slice)
SELECT id FROM items WHERE id IN (?1) ORDER BY id;

-- sgl.query(name: ItemsNotIn, command: many)
-- ?1 as sgl.param(ids, kind: slice)
SELECT id FROM items WHERE id NOT IN (?1) ORDER BY id;

-- sgl.query(name: ItemsWithMoods, command: many)
-- ?1 as sgl.param(moods, kind: slice)
SELECT id FROM items WHERE mood IN (?1) ORDER BY id;

-- sgl.query(name: ItemsWithoutMoods, command: many)
-- ?1 as sgl.param(moods, kind: slice)
SELECT id FROM items WHERE mood NOT IN (?1) ORDER BY id;

-- sgl.query(name: SetMood, command: execrows)
UPDATE items SET mood = ? WHERE id = ?;

-- sgl.query(name: GetItem, command: one)
SELECT id, kind, mood, score FROM items WHERE id = ?;

-- sgl.query(name: UpsertItems, command: batch)
INSERT INTO items (id, kind, score) VALUES (?, lower(?), ? + 1)
ON DUPLICATE KEY UPDATE kind = VALUES(kind), score = VALUES(score);

-- sgl.query(name: CopyEntries, command: copyfrom)
INSERT INTO entries (actor, action, id) VALUES (?, ?, ?);

-- sgl.query(name: ListEntries, command: many)
SELECT id, action, actor FROM entries ORDER BY id;
