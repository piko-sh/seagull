-- sgl.query(name: Stats, command: many)
SELECT *
FROM stats();

-- sgl.query(name: AnalyticsStats, command: many)
SELECT a
FROM analytics.stats();
