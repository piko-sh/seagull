-- sgl.query(AddItem, exec)
INSERT INTO menu_items (name, section, status, price_pence, nutrition) VALUES (?, ?, ?, ?, ?);

-- sgl.query(ListMenu, many)
-- sgl.column(kcal, type: integer)
SELECT name, status, price_pence, json_extract(nutrition, '$.kcal') AS kcal
FROM menu_items
ORDER BY section, name;

-- sgl.query(CheapestPerSection, many)
-- sgl.column(cheapest, go_type: "seagull.sh/seagull/examples/sqlite/009_overrides/internal/menu.Pence", nullable: false)
SELECT section, MIN(price_pence) AS cheapest
FROM menu_items
GROUP BY section
ORDER BY section;
