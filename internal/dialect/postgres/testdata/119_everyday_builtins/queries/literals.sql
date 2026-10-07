-- sgl.query(name: Ages, command: many)
SELECT id, age(created_at, '2024-01-01') AS since_new_year, date_bin('15 minutes', happened, '2000-01-01') AS slot
FROM events;

-- sgl.query(name: Roots, command: one)
SELECT cbrt(27.0) AS cube_root, sqrt(amount) AS exact_root, degrees(ratio) AS angle
FROM events
WHERE id = $1;

-- sgl.query(name: MedianRatio, command: one)
SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY ratio) AS median_ratio FROM events;

-- sgl.query(name: Retag, command: exec)
UPDATE events SET payload = jsonb_set(payload, '{status}', '"done"') WHERE id = $1;

-- sgl.query(name: LongIntervals, command: many)
SELECT id, make_interval(days => $1) AS span FROM events;
