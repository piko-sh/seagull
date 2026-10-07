-- sgl.query(name: UpdateJsonField, command: exec)
UPDATE events SET data = json_set(data, '$.processed', ?) WHERE id = ?;

-- sgl.query(name: RemoveJsonField, command: exec)
UPDATE events SET data = json_remove(data, '$.user.email') WHERE id = ?;

-- sgl.query(name: GetEventData, command: one)
SELECT id, data FROM events WHERE id = ?;

-- sgl.query(name: GetJsonType, command: one)
SELECT id, json_type(json_extract(data, '$.amount')) AS value_type FROM events WHERE id = ?;
