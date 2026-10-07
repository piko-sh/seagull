-- sgl.query(name: Differences, command: many)
SELECT id, due_on - ordered_on AS days_number, shipped_at - ordered_at AS shipped_number, NOW() - ordered_at AS age_number
FROM shipments;

-- sgl.query(name: Shifted, command: many)
SELECT id, ordered_at + INTERVAL 1 DAY AS follow_up, ordered_on - INTERVAL 1 DAY AS day_before
FROM shipments;
