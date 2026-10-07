-- sgl.query(RecordStock, exec)
INSERT INTO stock VALUES
    (1, map('k', (1, 'x'), 'j', (2, 'y')), [map('k', (2, 'y')), map()], map('k', [(5, 'v'), (6, 'w')]), map('o', map(7, (8, 'u'))),
     map(toDate('2024-05-06'), tuple(2)), [(3, map('z', tuple(4)))], (5, map('q', tuple(6))),
     map('r', (NULL, [1, NULL], toDateTime('2024-01-02 03:04:05'), toDecimal32(1.25, 2), NULL, toUUID('6ba7b810-9dad-11d1-80b4-00c04fd430c8'),
            'low', map('a', 1), toFixedString('ab', 2), toInt128('-170141183460469231731687303715884105728'), toIPv4('10.0.0.1'),
            toIPv6('::ffff:10.0.0.2'), true, 2.5, toDateTime64('2024-01-02 03:04:05.678', 3), (9, NULL))),
     map('p', (1, 'one')), (7, 'seven')),
    (2, map(), [], map(), map(), map(), [], (0, map()), map(), map(), (0, ''));

-- sgl.query(ListStock, many)
SELECT id, keyed, listed_keyed, keyed_lists, keyed_keyed, dated, listed_holders, holder.m AS held, rich, keyed_pairs, fields,
    CAST(map('k', (1, 'x')) AS Map(String, Tuple(a UInt8, b String))) AS built
FROM stock ORDER BY id;
