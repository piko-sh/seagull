-- sgl.query(StoredNests, many)
SELECT id, pairs_in_pair, pair_in_fields, listed, keyed, pair_list_in_pair, pair_in_fields_in_pair, pair_in_fields_in_fields, listed_fields, keyed_fields FROM nests;

-- sgl.query(BuiltNests, many)
SELECT (id, (id, 'gull')) AS parenthesised, tuple(id, tuple(id, 'gull')) AS called, [(id, (id, 'gull'))] AS listed FROM nests;

-- sgl.query(EveryNest, one)
SELECT * FROM nests WHERE id = {id:UInt32};
