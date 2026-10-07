-- sgl.query(AddApp, exec)
INSERT INTO apps (id, name) VALUES (?, ?);

-- sgl.query(CreateMissingAppItems, execrows)
INSERT INTO app_items (app_id, master_item_id)
SELECT a.id, ? FROM apps a
WHERE NOT EXISTS (SELECT 1 FROM app_items ai WHERE ai.app_id = a.id AND ai.master_item_id = ?);

-- sgl.query(CreateFirstAppItems, execrows)
INSERT INTO app_items (app_id, master_item_id)
SELECT a.id, ? FROM apps a
WHERE (SELECT count(*) FROM app_items ai WHERE ai.app_id = a.id) = 0;

-- sgl.query(ListAppItems, many)
SELECT app_id, master_item_id FROM app_items ORDER BY app_id, master_item_id;
