-- sgl.query(name: ShippingTimes, command: many)
SELECT id, shipped_at - ordered_at AS took, local_at - ordered_on AS since_order
FROM shipments
ORDER BY id;

-- sgl.query(name: DaysToDue, command: many)
SELECT id, due_on - ordered_on AS days
FROM shipments
ORDER BY id;

-- sgl.query(name: Deadlines, command: many)
SELECT id, ordered_on + 7 AS reminder_on, due_on - 1 AS last_day, ordered_on + handling AS ready_at,
       local_at - handling AS started_at, opens_at + handling AS closes_at, ordered_on + opens_at AS opens
FROM shipments
ORDER BY id;

-- sgl.query(name: ScaledHandling, command: many)
SELECT id, handling * 2 AS doubled, handling / 4 AS quarter
FROM shipments
ORDER BY id;
