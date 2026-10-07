-- sgl.query(AddBoat, exec)
INSERT INTO boats (id, name, home_port) VALUES ({id:UInt64}, {name:String}, {home_port:String});

-- sgl.query(ListBoats, many)
SELECT id, name, home_port FROM boats ORDER BY id;
