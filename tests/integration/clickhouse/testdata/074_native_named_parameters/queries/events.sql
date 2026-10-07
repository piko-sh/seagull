-- sgl.query(InsertEvent, exec)
INSERT INTO events (id, host, note, day, seen_at, in_stock, amount)
VALUES ({id:UInt64}, {host:String}, {note:Nullable(String)}, {day:Date}, {seen_at:DateTime64(3, 'UTC')}, {in_stock:Bool}, {amount:Decimal(18, 4)});

-- sgl.query(InsertEvents, batch)
INSERT INTO events (id, host, note, day, seen_at, in_stock, amount)
VALUES ({id:UInt64}, {host:String}, {note:Nullable(String)}, {day:Date}, {seen_at:DateTime64(3, 'UTC')}, {in_stock:Bool}, {amount:Decimal(18, 4)});

-- sgl.query(GetEvent, one)
SELECT id, host, note, day, seen_at, in_stock, amount FROM events WHERE id = {id:UInt64};

-- sgl.query(EventsByIDs, many)
SELECT id, host FROM events WHERE id IN {ids:Array(UInt64)} ORDER BY id;

-- sgl.query(PageEvents, many)
SELECT id FROM events ORDER BY id LIMIT {limit:UInt32} OFFSET {offset:UInt32};

-- sgl.query(EventsOnDay, many)
SELECT id FROM events WHERE day = {day:Date} AND amount >= {min_amount:Decimal(18, 4)} ORDER BY id;

-- sgl.query(EventsSeenSince, many)
SELECT id FROM events WHERE seen_at >= {since:DateTime64(3, 'UTC')} ORDER BY id;

-- sgl.query(EventsByHostOrNote, many)
SELECT id FROM events WHERE host = {text:String} OR note = {text:String} ORDER BY id;

-- sgl.query(EventsWithoutNote, many)
SELECT id FROM events WHERE isNull(note) AND in_stock = {in_stock:Bool} ORDER BY id;

-- sgl.query(name: SearchEvents, command: many, dynamic: runtime)
SELECT id, host FROM events WHERE in_stock = {only_stocked:Bool}
