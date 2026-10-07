-- sgl.query(name: ByFirstMood, command: many)
-- $1 as sgl.param(moods, kind: slice)
SELECT id FROM crossings WHERE moods[1] IN ($1);

-- sgl.query(name: ByMoods, command: many)
SELECT id FROM crossings WHERE moods[1] = $1 AND moods[2] = :second;

-- sgl.query(name: ByNestedMood, command: many)
SELECT id FROM crossings WHERE id IN (SELECT id FROM crossings WHERE moods[3] = :third);
