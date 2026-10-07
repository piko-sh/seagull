-- sgl.query(HatchGull, exec)
INSERT INTO gulls (id, name, parent_id, beach)
VALUES ({id:UInt64}, {name:String}, {parent_id:Nullable(UInt64)}, {beach:String});

-- sgl.query(RecordTheft, exec)
INSERT INTO thefts (id, gull_id, snack, stolen_on)
VALUES ({id:UInt64}, {gull_id:UInt64}, {snack:String}, {stolen_on:Date});

-- sgl.query(FamilyTree, many)
WITH RECURSIVE family AS (
    SELECT id, name, parent_id, 0 AS generation
    FROM gulls
    WHERE id = {root:UInt64}
    UNION ALL
    SELECT gulls.id, gulls.name, gulls.parent_id, family.generation + 1
    FROM gulls
    JOIN family ON gulls.parent_id = family.id
)
SELECT family.name, family.generation, parents.name AS parent
FROM family
LEFT JOIN gulls AS parents ON parents.id = family.parent_id
ORDER BY family.generation, family.id
SETTINGS join_use_nulls = 1;

-- sgl.query(TheftLeague, many)
WITH totals AS (
    SELECT gulls.name, gulls.beach, count(thefts.id) AS stolen
    FROM gulls
    LEFT JOIN thefts ON thefts.gull_id = gulls.id
    GROUP BY gulls.id, gulls.name, gulls.beach
)
SELECT
    name,
    beach,
    stolen,
    rank() OVER (PARTITION BY beach ORDER BY stolen DESC) AS beach_rank,
    sum(stolen) OVER (PARTITION BY beach) AS beach_thefts
FROM totals
ORDER BY beach, beach_rank, name
SETTINGS join_use_nulls = 1;

-- sgl.query(BeachRecords, many)
SELECT beach, gulls, thefts, last_theft FROM beach_records ORDER BY beach;
