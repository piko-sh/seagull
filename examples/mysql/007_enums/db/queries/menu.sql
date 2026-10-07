-- sgl.query(AddItem, exec)
INSERT INTO menu_items (name, section) VALUES (?, ?);

-- sgl.query(SetStatus, execrows)
UPDATE menu_items SET status = ? WHERE name = ?;

-- sgl.query(ListMenu, many)
SELECT name, section, status FROM menu_items ORDER BY section, name;

-- sgl.query(ItemsInSection, many)
-- sgl.column(section, go_type: "seagull.sh/seagull/examples/mysql/007_enums/internal/menu.Section")
SELECT name, section, status FROM menu_items WHERE section = ? ORDER BY name;
