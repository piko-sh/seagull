-- sgl.query(UnreadableValues, many)
SELECT keyed_wide, wide_keyed, listed_keyed_wide, held_keyed_wide, address_keyed, wide_pair, listed_wide_pairs, keyed_wide_pairs
FROM readings;

-- sgl.query(ReadableValues, many)
SELECT keyed_nullable_wide, keyed_wide_lists, keyed_wide_fields, keyed_addresses, nullable_wide_pair, address_pair,
    CAST(keyed_wide AS Map(String, Nullable(Int128))) AS cast_keyed_wide, mapKeys(wide_keyed) AS wide_keys,
    CAST(address_keyed AS Map(String, String)) AS cast_address_keyed, CAST(wide_pair AS Tuple(a UInt8, b Int128)) AS named_pair
FROM readings;
