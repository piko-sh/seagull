-- sgl.query(AddItem, exec)
INSERT INTO menu_items (name, section, status, price, nutrition) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(ListMenu, many)
-- sgl.column(kcal, type: bigint, nullable: false)
SELECT name, status, price, nutrition ->> 'kcal' AS kcal
FROM menu_items
ORDER BY section, name;

-- sgl.query(CheapestPerSection, many)
-- sgl.column(cheapest, go_type: "seagull.sh/seagull/examples/postgres/009_overrides/internal/menu.Price", nullable: false)
SELECT section, MIN(price) AS cheapest
FROM menu_items
GROUP BY section
ORDER BY section;
