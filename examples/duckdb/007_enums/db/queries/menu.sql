-- sgl.query(AddItem, exec)
INSERT INTO menu_items (name, section) VALUES ($1, $2);

-- sgl.query(SetStatus, execrows)
UPDATE menu_items SET status = $1 WHERE name = $2;

-- sgl.query(ListMenu, many)
SELECT name, section, status FROM menu_items ORDER BY section, name;

-- sgl.query(ItemsInSection, many)
-- sgl.column(section, go_type: "seagull.sh/seagull/examples/duckdb/007_enums/internal/menu.Section")
SELECT name, section, status FROM menu_items WHERE section = $1 ORDER BY name;
