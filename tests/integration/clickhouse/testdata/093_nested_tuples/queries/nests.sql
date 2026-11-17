-- sgl.query(RecordNests, exec)
INSERT INTO nests VALUES
    (1, (1, ('gull', 2)), (3, ('tern', 4)), (5, ('auk', 6)), (7, map('cod', ('many', 8))), (9, map('eel', ('few', 10))),
        [[(14, 'a'), (15, 'b')], []], (16, [('puffin', 17)]), map('ling', [(18, 'c')]), (19, [20, 21], 'tide')),
    (2, (22, ('skua', 23)), (24, ('kite', 25)), (26, ('wren', 27)), (28, map()), (29, map()), [], (31, []), map(), (32, [], NULL));

-- sgl.query(ReadableNests, many)
SELECT id, fields_in_pair, keyed_pairs_in_pair, keyed_pairs_in_fields, listed_lists, field_list_in_pair, keyed_lists, numbers_in_pair FROM nests ORDER BY id;

-- sgl.query(NamedNests, many)
SELECT id, CAST(pairs_in_pair AS Tuple(UInt8, Tuple(s String, n UInt16))) AS inner_named, CAST(pair_in_fields AS Tuple(a UInt8, b Tuple(s String, n UInt16))) AS all_named FROM nests ORDER BY id;

-- sgl.query(NestElements, many)
SELECT id, pairs_in_pair.2.1 AS inner_first, pairs_in_pair.2.2 AS inner_second, pair_in_fields.b AS inner_pair FROM nests ORDER BY id;
