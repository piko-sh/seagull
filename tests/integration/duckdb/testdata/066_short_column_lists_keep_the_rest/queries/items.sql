-- sgl.query(name: ViewItems, command: many)
SELECT * FROM renamed_items ORDER BY item_id;

-- sgl.query(name: CTEItems, command: many)
WITH renamed (item_id) AS (SELECT id, label, note FROM items) SELECT * FROM renamed ORDER BY item_id;

-- sgl.query(name: DerivedItems, command: many)
SELECT * FROM (SELECT id, label, note FROM items) AS renamed (item_id) ORDER BY item_id;
