-- sgl.query(Ranked, many)
SELECT name,
       rank() OVER (PARTITION BY beach ORDER BY id) AS beach_rank,
       row_number() OVER (ORDER BY id) AS position,
       dense_rank() OVER (ORDER BY beach) AS beach_number,
       sum(id) OVER (PARTITION BY beach) AS beach_total,
       count() OVER () AS gull_count
FROM gulls
ORDER BY id;

-- sgl.query(DistinctBeaches, one)
SELECT count(DISTINCT beach) AS beaches, uniqExact(name) AS names FROM gulls;
