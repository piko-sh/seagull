-- sgl.query(Doubled, one)
SELECT times_two_native(21) AS small, times_two_native(value) AS doubled, add_up_native(value, 0.5) AS added
FROM readings
WHERE id = {id:UInt64};
