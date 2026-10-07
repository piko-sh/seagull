-- sgl.query(Insert, exec)
INSERT INTO sessions (id, label) VALUES ({id:UUID}, {label:String});

-- sgl.query(Get, one)
SELECT id, label FROM sessions WHERE id = {id:UUID};
