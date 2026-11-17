-- sgl.query(name: ListFeelings, command: many)
SELECT * FROM feelings;

-- sgl.query(name: FeelingsIn, command: many)
SELECT id FROM feelings WHERE mood = $1;
