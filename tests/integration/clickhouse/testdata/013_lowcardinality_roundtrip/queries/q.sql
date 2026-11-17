-- sgl.query(Insert, exec)
INSERT INTO events (id, country) VALUES ({id:UInt64}, {country:String});

-- sgl.query(List, many)
SELECT id, country FROM events ORDER BY id;
