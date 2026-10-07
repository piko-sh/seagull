-- sgl.query(InsertItem, exec)
INSERT INTO items (id, mood, maybe, moods, maybes, labels)
VALUES ({id:UInt64}, {mood:Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)}, {maybe:Nullable(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3))}, {moods:Array(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3))}, {maybes:Array(Nullable(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)))}, {labels:Map(String, Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3))});

-- sgl.query(InsertItems, batch)
INSERT INTO items (id, mood, maybe, moods, maybes, labels)
VALUES ({id:UInt64}, {mood:Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)}, {maybe:Nullable(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3))}, {moods:Array(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3))}, {maybes:Array(Nullable(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)))}, {labels:Map(String, Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3))});

-- sgl.query(GetItem, one)
SELECT id, mood, maybe, moods, maybes, labels FROM items WHERE id = {id:UInt64};

-- sgl.query(ItemsWithMood, many)
SELECT id FROM items WHERE mood = {mood:Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)} ORDER BY id;

-- sgl.query(ItemsWithMoods, many)
-- {moods:Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)} as sgl.param(moods, kind: slice)
SELECT id FROM items WHERE mood IN ({moods:Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)}) ORDER BY id;

-- sgl.query(ItemsWithoutMoods, many)
-- {moods:Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)} as sgl.param(moods, kind: slice)
SELECT id FROM items WHERE mood NOT IN ({moods:Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)}) ORDER BY id;

-- sgl.query(ItemsMaybe, many)
SELECT id FROM items WHERE maybe = {maybe:Nullable(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3))} ORDER BY id;
