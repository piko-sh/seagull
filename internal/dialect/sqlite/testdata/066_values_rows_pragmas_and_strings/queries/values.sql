-- sgl.query(name: Countdown, command: many)
WITH RECURSIVE c(n) AS (VALUES (3) UNION ALL SELECT n - 1 FROM c WHERE n > 1) SELECT n FROM c;

-- sgl.query(name: Pairs, command: many)
WITH pairs(code, label) AS (VALUES (1, 'one'), (2, 'two')) SELECT code, label FROM pairs;

-- sgl.query(name: Mixed, command: many)
VALUES (0, 'none') UNION ALL SELECT id, category FROM ledger;
