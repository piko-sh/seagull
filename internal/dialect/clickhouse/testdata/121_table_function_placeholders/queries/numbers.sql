-- sgl.query(name: FirstNumbers, command: many)
SELECT number FROM numbers({count:UInt64});

-- sgl.query(name: NumbersFrom, command: many)
SELECT number FROM numbers({start:UInt64}, {count:UInt64}) WHERE number > {start:UInt64};
