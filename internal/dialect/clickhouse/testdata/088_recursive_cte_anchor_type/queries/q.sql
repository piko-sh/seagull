-- sgl.query(Family, many)
WITH RECURSIVE family AS (
    SELECT id, 0 AS generation FROM gulls WHERE id = {root:UInt64}
    UNION ALL
    SELECT gulls.id, family.generation + 1 FROM gulls JOIN family ON gulls.parent_id = family.id
)
SELECT id, generation FROM family;
