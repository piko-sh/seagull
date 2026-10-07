-- sgl.query(RecordEvents, exec)
INSERT INTO events VALUES (1, 42, '{"a": 1, "b": {"c": "x"}}', [1, 'two']), (2, 'hello', '{"a": "text"}', []), (3, NULL, '{}', [NULL]);

-- sgl.query(Events, many)
SELECT id, payload, data.a AS a, data.b.c AS c, extras FROM events ORDER BY id;
