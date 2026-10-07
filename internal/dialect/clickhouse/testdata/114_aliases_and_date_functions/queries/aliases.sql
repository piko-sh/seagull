-- sgl.query(name: Cleaned, command: many)
SELECT replace(path, '/', '-') AS slug, trim(kind) AS tidy, lcase(kind) AS lowered, substr(path, 2) AS tail,
       date_diff('day', at, now()) AS age_days, YEAR(at) AS year, MONTH(day) AS month, DATE_FORMAT(at, '%Y') AS year_text
FROM events;

-- sgl.query(name: Totals, command: many)
SELECT user_id, any_value(kind) AS some_kind, group_concat(kind) AS kinds, array_agg(path) AS paths,
       medianExact(bytes) AS median_bytes, medianExactIf(bytes, kind = 'view') AS median_view_bytes,
       medianTDigest(duration) AS median_duration, BIT_AND(flags) AS common_flags, STDDEV_POP(duration) AS spread
FROM events
GROUP BY user_id;
