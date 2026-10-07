CREATE TABLE nests (
    id UInt32,
    pairs_in_pair Tuple(UInt8, Tuple(String, UInt16)),
    pair_in_fields Tuple(a UInt8, b Tuple(String, UInt16)),
    listed Array(Tuple(UInt8, Tuple(String, UInt16))),
    keyed Map(String, Tuple(UInt8, Tuple(String, UInt16))),
    pair_list_in_pair Tuple(UInt8, Array(Tuple(String, UInt16))),
    pair_in_fields_in_pair Tuple(UInt8, Tuple(s String, n Tuple(UInt8, String))),
    pair_in_fields_in_fields Tuple(a UInt8, b Tuple(c UInt8, d Tuple(String, UInt16))),
    listed_fields Array(Tuple(a UInt8, b Tuple(String, UInt16))),
    keyed_fields Map(String, Tuple(a UInt8, b Tuple(String, UInt16)))
) ENGINE = MergeTree ORDER BY id;
