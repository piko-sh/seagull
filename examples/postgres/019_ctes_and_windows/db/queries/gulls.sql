-- sgl.query(HatchGull, exec)
INSERT INTO gulls (name, parent_id, beach) VALUES ($1, $2, $3);

-- sgl.query(RecordTheft, exec)
INSERT INTO thefts (gull_id, snack, stolen_on) VALUES ($1, $2, $3);

-- sgl.query(FamilyTree, many)
WITH RECURSIVE descendants AS (
    SELECT id, name, parent_id, 0::BIGINT AS generation
    FROM gulls
    WHERE id = $1
    UNION ALL
    SELECT gulls.id, gulls.name, gulls.parent_id, descendants.generation + 1
    FROM gulls
    JOIN descendants ON gulls.parent_id = descendants.id
)
SELECT descendants.name, descendants.generation, parents.name AS parent
FROM descendants
LEFT JOIN gulls AS parents ON parents.id = descendants.parent_id
ORDER BY descendants.generation, descendants.id;

-- sgl.query(TheftLeague, many)
WITH totals AS (
    SELECT gulls.name, gulls.beach, COUNT(thefts.id) AS thefts
    FROM gulls
    LEFT JOIN thefts ON thefts.gull_id = gulls.id
    GROUP BY gulls.id
)
SELECT
    name,
    beach,
    thefts,
    RANK() OVER (PARTITION BY beach ORDER BY thefts DESC) AS beach_rank,
    SUM(thefts) OVER (PARTITION BY beach) AS beach_thefts
FROM totals
ORDER BY beach, beach_rank, name;

-- sgl.query(BeachRecords, many)
SELECT * FROM beach_records ORDER BY beach;
