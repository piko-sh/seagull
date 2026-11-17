-- sgl.query(InsertGull, exec)
INSERT INTO gulls (id, wing, span, ringed, tagged)
VALUES ({id:UInt64}, {wing:Int32}, {span:Nullable(Int32)}, {ringed:Bool}, {tagged:Nullable(Bool)});

-- sgl.query(GullPredicates, many)
SELECT id, wing > 40 AS long_wing, span > 100 AS wide, span IS NULL AS unmeasured, wing IN (40, 42) AS listed,
    span BETWEEN 90 AND 110 AS typical, wing > 40 AND ringed AS long_and_ringed, tagged OR ringed AS marked,
    NOT (wing > 40) AS short_wing, EXISTS (SELECT 1) AS present
FROM gulls ORDER BY id;
