-- sgl.query(HatchGull, exec)
INSERT INTO gulls (name, parent_id, beach) VALUES (?, ?, ?);

-- sgl.query(RecordTheft, exec)
INSERT INTO thefts (gull_id, snack, stolen_on) VALUES (?, ?, ?);

-- sgl.query(FamilyTree, many)
WITH RECURSIVE family AS (
    SELECT id, name, parent_id, 0 AS generation
    FROM gulls
    WHERE id = ?
    UNION ALL
    SELECT gulls.id, gulls.name, gulls.parent_id, family.generation + 1
    FROM gulls
    JOIN family ON gulls.parent_id = family.id
)
SELECT family.name, family.generation, parents.name AS parent
FROM family
LEFT JOIN gulls AS parents ON parents.id = family.parent_id
ORDER BY family.generation, family.id;

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
SELECT beach, gulls, thefts, last_theft FROM beach_records ORDER BY beach;
