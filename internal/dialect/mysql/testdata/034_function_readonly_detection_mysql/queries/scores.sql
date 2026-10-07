-- sgl.query(name: ReadOnlyWithDeterministic, command: many)
SELECT id, safe_multiply(points, 2) AS doubled FROM scores;

-- sgl.query(name: NotReadOnlyWithUnknown, command: one)
SELECT some_unknown_function(?) AS result;
