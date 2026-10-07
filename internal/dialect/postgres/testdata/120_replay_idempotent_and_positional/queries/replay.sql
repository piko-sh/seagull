-- sgl.query(name: ListItems, command: many)
SELECT * FROM items;

-- sgl.query(name: ListLogs, command: many)
SELECT * FROM logs;

-- sgl.query(name: ListPairs, command: many)
SELECT p.left_id, p.right_id, l.left_id AS later_left_id FROM pairs p JOIN later_pairs l ON l.right_id = p.right_id;

-- sgl.query(name: ListChildren, command: many)
SELECT * FROM child;

-- sgl.query(name: ListCopied, command: many)
SELECT * FROM copied;

-- sgl.query(name: ListViews, command: many)
SELECT s.id, s.item_name, q.name, n.item_id, n.item_name AS named_name FROM star_view s JOIN qualified_view q ON q.id = s.id JOIN named_view n ON n.item_id = s.id;

-- sgl.query(name: ListSnapshot, command: many)
SELECT * FROM archive.snapshot;
