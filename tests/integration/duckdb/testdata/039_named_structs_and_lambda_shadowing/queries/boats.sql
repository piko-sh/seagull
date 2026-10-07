-- sgl.query(InsertBoat, exec)
INSERT INTO boats (id, name, score, scores) VALUES ($1, $2, $3, $4);

-- sgl.query(BoatStructs, many)
SELECT struct_pack(id := id, name := name) AS packed, {'id': id, 'name': name} AS literal, [{'score': id}] AS listed
FROM boats
ORDER BY id;

-- sgl.query(HighScoringBoats, many)
SELECT id
FROM boats
WHERE len(list_filter(scores, score -> score > $1)) > 0
ORDER BY id;

-- sgl.query(BoatsScoredAbove, many)
SELECT id
FROM boats
WHERE len([score FOR score IN scores IF score > $1]) > 0 AND score = $2
ORDER BY id;
