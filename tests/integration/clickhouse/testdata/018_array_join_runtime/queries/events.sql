-- sgl.query(InsertEvent, exec)
INSERT INTO events (id, tags) VALUES ({id:UInt64}, {tags:Array(String)});

-- sgl.query(ByTag, many)
SELECT id, tag FROM events ARRAY JOIN tags AS tag ORDER BY id, tag;
