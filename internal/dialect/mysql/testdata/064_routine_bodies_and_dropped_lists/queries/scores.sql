-- sgl.query(name: BumpScores, command: many)
SELECT bump_if(points) AS bumped FROM scores;

-- sgl.query(name: LabelScores, command: many)
SELECT label_of(points) AS label FROM scores;

-- sgl.query(name: ReadDroppedTable, command: many)
SELECT id FROM legacy_b;

-- sgl.query(name: ReadDroppedView, command: many)
SELECT id FROM vb;
