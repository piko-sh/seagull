-- sgl.query(Walk, many)
WITH RECURSIVE walk(id, name, ancestor) AS (
    SELECT id, name, name FROM nodes WHERE parent_id IS NULL
    UNION ALL
    SELECT n.id, n.name, p.name FROM nodes n JOIN walk w ON n.parent_id = w.id LEFT JOIN nodes p ON p.id = n.parent_id + 1000
)
SELECT id, name, ancestor FROM walk;

-- sgl.query(Shift, many)
WITH RECURSIVE chain(n, a, b) AS (
    SELECT 1, 'x'::text, 'y'::text
    UNION ALL
    SELECT n + 1, b, NULL::text FROM chain WHERE n < 3
)
SELECT n, a, b FROM chain;
