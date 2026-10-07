-- sgl.query(InsertEvent, exec)
INSERT INTO events (id, occurred_at) VALUES ({id:UInt64}, {occurred_at:DateTime64(6, 'UTC')});

-- sgl.query(GetEvent, one)
SELECT id, occurred_at FROM events WHERE id = {id:UInt64};

-- sgl.query(ListEvents, many)
SELECT id, occurred_at FROM events ORDER BY id;
