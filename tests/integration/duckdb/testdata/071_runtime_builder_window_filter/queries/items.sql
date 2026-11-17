-- sgl.query(name: RankedItems, command: many, dynamic: runtime)
SELECT id, grp, row_number() OVER (PARTITION BY grp ORDER BY id) AS rn FROM items;
