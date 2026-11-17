-- sgl.query(name: PickUnknown, command: one)
SELECT pick($1) AS picked;

-- sgl.query(name: AbsUnknown, command: one)
SELECT abs($1) AS absolute;

-- sgl.query(name: WeighUnknown, command: one)
SELECT weigh($1) AS weighed;

-- sgl.query(name: PickTyped, command: one)
SELECT pick($1::TEXT) AS picked;
