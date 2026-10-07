-- sgl.query(name: GetEventName, command: many)
SELECT id, payload->>'name' AS event_name FROM events;

-- sgl.query(name: GetEventMetadata, command: many)
SELECT id, payload->'metadata' AS metadata FROM events;
