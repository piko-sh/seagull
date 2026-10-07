-- sgl.query(ItemsIn, many)
-- $1 as sgl.param(ids, kind: slice)
SELECT id FROM items WHERE id IN ($1) ORDER BY id;

-- sgl.query(ItemsNotIn, many)
-- $1 as sgl.param(ids, kind: slice)
SELECT id FROM items WHERE id NOT IN ($1) ORDER BY id;

-- sgl.query(ItemsWithMoods, many)
-- $1 as sgl.param(moods, kind: slice)
SELECT id FROM items WHERE mood IN ($1) ORDER BY id;

-- sgl.query(ItemsWithoutMoods, many)
-- $1 as sgl.param(moods, kind: slice)
SELECT id FROM items WHERE mood NOT IN ($1) ORDER BY id;

-- sgl.query(SetMood, execrows)
UPDATE items SET mood = $1 WHERE id = $2;

-- sgl.query(SetMoods, execrows)
UPDATE items SET moods = $1 WHERE id = $2;

-- sgl.query(GetItem, one)
SELECT id, kind, mood, moods, rank FROM items WHERE id = $1;

-- sgl.query(UpsertItems, batch)
INSERT INTO items (id, kind, rank) VALUES ($1, lower($2), $3 + 1)
ON CONFLICT (id) DO UPDATE SET kind = EXCLUDED.kind, rank = EXCLUDED.rank;

-- sgl.query(InsertMoodyItems, batch)
INSERT INTO items (id, kind, moods) VALUES ($1, $2, $3);

-- sgl.query(CopyEntries, copyfrom)
INSERT INTO audit_sql.entries (actor, action, id) VALUES ($1, $2, $3);

-- sgl.query(ListEntries, many)
SELECT id, action, actor FROM audit_sql.entries ORDER BY id;
