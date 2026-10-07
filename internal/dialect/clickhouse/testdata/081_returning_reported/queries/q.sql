-- sgl.query(RingGull, one)
INSERT INTO gulls (id, name) VALUES ({id:UInt64}, {name:String}) RETURNING id;
