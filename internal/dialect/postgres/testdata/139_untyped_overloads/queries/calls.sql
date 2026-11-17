-- sgl.query(name: ScaleUnknown, command: one)
SELECT scale($1) AS digits;

-- sgl.query(name: PickUnknown, command: one)
SELECT pick($1) AS picked;

-- sgl.query(name: WeighUnknown, command: one)
SELECT weigh($1) AS weighed;

-- sgl.query(name: ScaleTyped, command: one)
SELECT scale($1::integer) AS digits;
