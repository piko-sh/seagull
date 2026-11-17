-- sgl.query(name: ShippingTimes, command: many)
SELECT id, shipped_at - ordered_at AS took, now() - ordered_at AS age, local_at - ordered_on AS since_order
FROM shipments;

-- sgl.query(name: DaysToDue, command: many)
SELECT id, due_on - ordered_on AS days, current_date - due_on AS overdue
FROM shipments;

-- sgl.query(name: Deadlines, command: many)
SELECT id, ordered_on + 7 AS reminder_on, due_on - 1 AS last_day, ordered_on + handling AS ready_at,
       ordered_at + interval '1 day' AS follow_up, local_at - handling AS started_at, opens_at + handling AS closes_at,
       ordered_on + opens_at AS opens
FROM shipments;

-- sgl.query(name: ScaledHandling, command: many)
SELECT id, handling * 2 AS doubled, handling / 4 AS quarter
FROM shipments;
