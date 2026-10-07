-- sgl.query(name: ShippingTimes, command: many)
SELECT id, shipped_at - ordered_at AS took, ordered_at - ordered_on AS since_midnight
FROM shipments;

-- sgl.query(name: DaysToDue, command: many)
SELECT id, due_on - ordered_on AS days
FROM shipments;

-- sgl.query(name: Deadlines, command: many)
SELECT id, ordered_on + 7 AS reminder_on, due_on - 1 AS last_day, ordered_on + handling AS ready_at,
       ordered_at - INTERVAL 1 DAY AS day_before, handling * 2 AS doubled
FROM shipments;
