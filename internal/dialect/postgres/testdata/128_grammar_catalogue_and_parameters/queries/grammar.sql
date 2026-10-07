-- sgl.query(name: Labels, command: many)
VALUES (0, 'none') UNION ALL SELECT id, label FROM places ORDER BY 1;

-- sgl.query(name: Countdown, command: many)
WITH RECURSIVE c(n) AS (VALUES (3) UNION ALL SELECT n - 1 FROM c WHERE n > 1) SELECT n FROM c;

-- sgl.query(name: Extremes, command: many)
SELECT x.id FROM ((SELECT id FROM places ORDER BY id LIMIT 1) UNION (SELECT id FROM places ORDER BY id DESC LIMIT 1)) AS x;

-- sgl.query(name: EveryMark, command: many)
SELECT t.id, t.row FROM (TABLE marks) AS t WHERE EXISTS (TABLE places);

-- sgl.query(name: StockedItems, command: many)
SELECT inventory.items.* FROM inventory.items WHERE inventory.items.price > $1;

-- sgl.query(name: MarkRows, command: many)
SELECT row.id, row.row FROM marks AS row WHERE row.row = $1;

-- sgl.query(name: Renamed, command: many)
SELECT p.key, p.spot, p.label FROM places AS p (key, spot) WHERE p.key = $1;

-- sgl.query(name: Page, command: many)
SELECT id FROM places ORDER BY id LIMIT :size::int OFFSET (:page::int - 1) * :size;

-- sgl.query(name: Nearest, command: many)
SELECT id, location <-> point(0, 0) AS distance, query <-> query AS followed, label <-> 'x' AS trigram_distance
FROM places ORDER BY location <-> point(0, 0) LIMIT 5;
