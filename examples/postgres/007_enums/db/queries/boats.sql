-- sgl.query(AddBoat, exec)
INSERT INTO boats (name, status) VALUES ($1, $2);

-- sgl.query(SetStatus, execrows)
UPDATE boats SET status = $1 WHERE name = $2;

-- sgl.query(ListBoats, many)
SELECT name, status FROM boats ORDER BY id;

-- sgl.query(BoatsWithStatus, many)
-- sgl.column(status, go_type: "seagull.sh/seagull/examples/postgres/007_enums/internal/harbour.Status")
SELECT name, status FROM boats WHERE status = $1 ORDER BY id;
