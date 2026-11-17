-- sgl.query(name: CrewCounts, command: many)
SELECT boats.id, (SELECT count() FROM crew WHERE crew.boat = boats.name) AS crew_count,
       (SELECT sum(crew.id) FROM crew WHERE crew.boat = boats.name) AS crew_total
FROM boats;

-- sgl.query(name: UncorrelatedCount, command: many)
SELECT boats.id, (SELECT count() FROM crew WHERE crew.boat = 'none') AS crew_count FROM boats;

-- sgl.query(name: OuterTableName, command: many)
SELECT id, (SELECT count() FROM crew AS mates WHERE mates.boat = crew.boat) AS mates FROM crew;

-- sgl.query(name: AliasedTableByName, command: many)
SELECT crew.boat FROM crew AS mates;
