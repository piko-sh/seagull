-- sgl.query(AddBoat, exec)
INSERT INTO boats (id, name, status) VALUES ({id:UInt64}, {name:String}, {status:String});

-- sgl.query(ListBoats, many)
SELECT id, name, status FROM boats ORDER BY id;

-- sgl.query(BoatsNotIn, many)
-- sgl.column(status, go_type: "seagull.sh/seagull/examples/clickhouse/007_enums/internal/domain.Status")
SELECT id, name, status FROM boats WHERE status != {status:String} ORDER BY id;
