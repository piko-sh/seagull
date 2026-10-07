-- sgl.query(RecordSwitches, exec)
INSERT INTO switches (id, flags, ratios, nested) VALUES ({id:UInt64}, {true: false}, {true: inf, false: NULL}, {true: {false: true}}), (2, {TRUE: FALSE, false: true}, {}, {false: {true: false}, true: {}});

-- sgl.query(Switches, many)
SELECT id, flags, ratios, nested FROM switches ORDER BY id;
