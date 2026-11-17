-- sgl.query(InsertEvent, exec)
INSERT INTO events (id, host) VALUES ({id:UInt64}, {host:String});

-- sgl.query(PageEvents, many)
SELECT id, host
FROM events
ORDER BY id
LIMIT {limit:UInt32} OFFSET {offset:UInt32};
