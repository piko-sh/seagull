-- sgl.query(name: NumberItems, command: many, dynamic: runtime)
SELECT i.id, i.grp, row_number() OVER (PARTITION BY i.grp ORDER BY i.id) AS rn
FROM items i
WHERE i.score IS NOT NULL
ORDER BY i.id;

-- sgl.query(name: GroupTotals, command: many, dynamic: runtime)
SELECT grp, count(*) AS total FROM items GROUP BY grp;

-- sgl.query(name: PlainItems, command: many, dynamic: runtime)
SELECT id, grp FROM items WHERE score > $1;
