-- sgl.query(UndecodableKeys, many)
SELECT list_keyed, pair_keyed, unnamed_keyed, map_keyed, decimal_keyed, listed_decimal_keyed, held_list_keyed
FROM lookups;

-- sgl.query(DecodableKeys, many)
SELECT uuid_keyed, date_keyed, text_keyed, mapKeys(list_keyed) AS list_keys, mapValues(decimal_keyed) AS decimal_values,
    mapKeys(pair_keyed) AS pair_keys, CAST(decimal_keyed AS Map(String, String)) AS cast_decimal_keyed
FROM lookups;
