-- sgl.query(name: ListItems, command: many)
SELECT * FROM items ORDER BY id;

-- sgl.query(name: ListDiary, command: many)
SELECT * FROM diary ORDER BY id;

-- sgl.query(name: ListPairs, command: many)
SELECT * FROM pairs ORDER BY left_id;

-- sgl.query(name: ListLaterPairs, command: many)
SELECT * FROM later_pairs ORDER BY left_id;

-- sgl.query(name: ListChildren, command: many)
SELECT * FROM child ORDER BY id;

-- sgl.query(name: ListCopied, command: many)
SELECT * FROM copied ORDER BY id;

-- sgl.query(name: ListStarView, command: many)
SELECT * FROM star_view ORDER BY id;

-- sgl.query(name: ListNamedView, command: many)
SELECT * FROM named_view ORDER BY item_id;

-- sgl.query(name: ListSnapshot, command: many)
SELECT * FROM snapshot ORDER BY id;
