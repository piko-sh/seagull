-- sgl.query(name: EachArm, command: many)
SELECT id FROM events WHERE kind = {first:String}
UNION ALL
SELECT id FROM events WHERE kind = {second:String};

-- sgl.query(name: SecondArmOnly, command: many)
SELECT id FROM events
UNION ALL
SELECT id FROM events WHERE score > {floor:Int32};

-- sgl.query(name: ThirdArmOfAChain, command: many)
SELECT id FROM events
UNION DISTINCT
SELECT id FROM events WHERE kind = {kind:String}
EXCEPT
SELECT id FROM events WHERE score < {ceiling:Int32};

-- sgl.query(name: RepeatedAcrossArms, command: many)
SELECT id FROM events WHERE id = {id:UInt64}
UNION ALL
SELECT id FROM events WHERE id = {id:UInt64} + 1;

-- sgl.query(name: ArmsOfADerivedTable, command: many)
SELECT id FROM (
    SELECT id FROM events
    UNION ALL
    SELECT id FROM events WHERE kind = {kind:String}
) WHERE id > {after:UInt64};

-- sgl.query(name: ArmsOfACTE, command: many)
WITH found AS (
    SELECT id FROM events
    UNION ALL
    SELECT id FROM events WHERE score = {score:Int32}
)
SELECT id FROM found;
