-- sgl.query(AddCrew, exec)
INSERT INTO crews (id, names, ranks) VALUES ($1, $2, $3);

-- sgl.query(GetCrew, one)
SELECT id, names IS NULL AS missing, coalesce(len(names), -1) AS name_count, len(ranks) AS rank_count FROM crews WHERE id = $1;
