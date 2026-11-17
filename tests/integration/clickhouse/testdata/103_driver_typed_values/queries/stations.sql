-- sgl.query(RecordStations, exec)
INSERT INTO stations VALUES
    (1, '10.0.0.1', '10.0.0.13', toInt128('-170141183460469231731687303715884105728'),
     ('10.0.0.2', '::ffff:10.0.0.3', NULL, toInt128('170141183460469231731687303715884105727'), NULL,
      toUInt256('115792089237316195423570985008687907853269984665640564039457584007913129639935'),
      ['10.0.0.4'], map('m', '10.0.0.5'), ('10.0.0.6', -5), [(7, '2001:db8::1')]),
     [(8, '10.0.0.7', 9)], ['10.0.0.8', '10.0.0.9'], ['10.0.0.10', NULL], map('v6', '::1'), [['10.0.0.11'], []],
     (10, map('z', tuple(11)))),
    (2, '0.0.0.0', NULL, 0, ('0.0.0.0', '::', '10.0.0.12', 0, 12, 0, [], map(), ('0.0.0.0', 0), []), [], [], [], map(), [],
     (0, map()));

-- sgl.query(ListStations, many)
SELECT id, address, optional, wide, addressed, listed, addresses, nullable_addresses, keyed_addresses, nested_addresses, holder
FROM stations ORDER BY id;

-- sgl.query(GetStation, one)
SELECT addressed, holder FROM stations WHERE id = {id:UInt64};

-- sgl.query(StreamStations, stream)
SELECT id, addressed FROM stations ORDER BY id;
