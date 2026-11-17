-- sgl.query(ReadableNests, many)
SELECT id, fields_in_pair, keyed_pairs_in_pair, pair_list_in_fields, keyed_pairs_in_fields, numbers_in_pair, listed_lists, field_list_in_pair, keyed_lists FROM nests;

-- sgl.query(NamedNests, many)
SELECT CAST(pairs_in_pair AS Tuple(UInt8, Tuple(s String, n UInt16))) AS inner_named, CAST(pair_in_fields AS Tuple(a UInt8, b Tuple(s String, n UInt16))) AS all_named, pairs_in_pair::Tuple(x UInt8, y Tuple(s String, n UInt16)) AS cast_named FROM nests;

-- sgl.query(NestElements, many)
SELECT pairs_in_pair.1 AS first, pairs_in_pair.2.1 AS inner_first, pairs_in_pair.2.2 AS inner_second, pair_in_fields.b AS inner_pair FROM nests;
