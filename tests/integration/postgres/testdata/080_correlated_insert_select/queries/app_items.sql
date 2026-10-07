-- sgl.query(AddApp, exec)
INSERT INTO apps (id, name) VALUES ($1, $2);

-- sgl.query(CreateMissingAppItems, execrows)
INSERT INTO app_items (app_id, master_item_id)
SELECT a.id, $1 FROM apps a
WHERE NOT EXISTS (
    SELECT 1 FROM app_items ai
    WHERE ai.app_id = a.id AND ai.master_item_id = $1
);

-- sgl.query(CreateFirstAppItems, execrows)
INSERT INTO app_items (app_id, master_item_id)
SELECT a.id, $1 FROM apps a
WHERE (SELECT count(*) FROM app_items ai WHERE ai.app_id = a.id) = 0;

-- sgl.query(CopyItemCounts, execrows)
INSERT INTO app_items (app_id, master_item_id)
SELECT a.id, l.items FROM apps a, LATERAL (SELECT count(*) AS items FROM app_items ai WHERE ai.app_id = a.id) l
WHERE a.id = $1;

-- sgl.query(ListAppItems, many)
SELECT app_id, master_item_id FROM app_items ORDER BY app_id, master_item_id;
