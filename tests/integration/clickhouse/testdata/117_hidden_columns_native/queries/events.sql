-- sgl.query(name: ListEvents, command: many)
SELECT * FROM events ORDER BY id;

-- sgl.query(name: ListQualifiedEvents, command: many)
SELECT e.* FROM events AS e ORDER BY e.id;

-- sgl.query(name: ListComputed, command: many)
SELECT id, doubled, loud FROM events ORDER BY id;
