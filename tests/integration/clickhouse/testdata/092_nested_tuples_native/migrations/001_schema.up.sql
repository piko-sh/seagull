CREATE TABLE nests (
    id UInt16,
    pairs_in_pair Tuple(UInt8, Tuple(String, UInt16)),
    pair_in_fields Tuple(a UInt8, b Tuple(String, UInt16)),
    fields_in_pair Tuple(UInt8, Tuple(s String, n UInt16)),
    keyed_pairs_in_pair Tuple(UInt8, Map(String, Tuple(String, UInt16))),
    keyed_pairs_in_fields Tuple(a UInt8, b Map(String, Tuple(String, UInt16))),
    pair_list_in_fields Tuple(a UInt8, b Array(Tuple(String, UInt16))),
    listed_lists Array(Array(Tuple(UInt8, String))),
    field_list_in_pair Tuple(UInt8, Array(Tuple(s String, n UInt16))),
    keyed_lists Map(String, Array(Tuple(UInt8, String))),
    numbers_in_pair Tuple(UInt8, Array(UInt16), Nullable(String))
) ENGINE = MergeTree ORDER BY id;
