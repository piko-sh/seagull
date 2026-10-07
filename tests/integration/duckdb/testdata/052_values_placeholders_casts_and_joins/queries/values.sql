-- sgl.query(name: Countdown, command: many)
WITH RECURSIVE c(n) AS (VALUES (3) UNION ALL SELECT n - 1 FROM c WHERE n > 1) SELECT n FROM c;

-- sgl.query(name: Mixed, command: many)
SELECT id, city FROM trips UNION ALL VALUES (0, 'none') ORDER BY 1;

-- sgl.query(name: Literal, command: many)
VALUES (1, 'one'), (2, 'two');
