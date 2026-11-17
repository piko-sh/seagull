-- sgl.query(InsertEvents, batch)
INSERT INTO events (id, host, note, day) VALUES ({id:UInt64}, lower({host:String}), {note:Nullable(String)}, {day:Date});

-- sgl.query(CopyEvents, copyfrom)
INSERT INTO events (id, host, note, day) VALUES ({id:UInt64}, {host:String}, {note:Nullable(String)}, {day:Date});

-- sgl.query(ListEventsBelow, many)
SELECT id, host, note, day FROM events WHERE id < {below:UInt64} ORDER BY id;

-- sgl.query(SummariseEvents, one)
SELECT count() AS total, uniqExact(host) AS hosts, min(id) AS lowest, max(id) AS highest, countIf(isNull(note)) AS without_note FROM events WHERE id >= {from:UInt64};
