-- sgl.query(RecordProbe, exec)
INSERT INTO probes (id, label, amount, ratio, flag, note, payload, optional_label, optional_amount, tags, host, optional_host, address, pair, keyed)
VALUES ({id:Int32}, {label:String}, {amount:Int64}, {ratio:Float64}, {flag:Bool}, {note:String}, {payload:String}, {optional_label:Nullable(String)}, {optional_amount:Nullable(Int64)}, {tags:Array(String)}, {host:IPv4}, {optional_host:Nullable(IPv4)}, {address:IPv4}, {pair:Tuple(name String, rank Int64)}, {keyed:Map(String, Int64)});

-- sgl.query(RecordProbes, batch)
INSERT INTO probes (id, label, amount, ratio, flag, note, payload, optional_label, optional_amount, tags, host, optional_host, address, pair, keyed)
VALUES ({id:Int32}, {label:String}, {amount:Int64}, {ratio:Float64}, {flag:Bool}, {note:String}, {payload:String}, {optional_label:Nullable(String)}, {optional_amount:Nullable(Int64)}, {tags:Array(String)}, {host:IPv4}, {optional_host:Nullable(IPv4)}, {address:IPv4}, {pair:Tuple(name String, rank Int64)}, {keyed:Map(String, Int64)});

-- sgl.query(RecordProbeRows, batch)
INSERT INTO probes (id, label, amount, ratio, flag, note, payload, optional_label, optional_amount, tags, host, optional_host, address, pair, keyed)
VALUES ({id:Int32} + 0, {label:String}, {amount:Int64}, {ratio:Float64}, {flag:Bool}, {note:String}, {payload:String}, {optional_label:Nullable(String)}, {optional_amount:Nullable(Int64)}, {tags:Array(String)}, {host:IPv4}, {optional_host:Nullable(IPv4)}, {address:IPv4}, {pair:Tuple(name String, rank Int64)}, {keyed:Map(String, Int64)});

-- sgl.query(FindByLabel, many)
SELECT id FROM probes WHERE label = {label:String} ORDER BY id;

-- sgl.query(FindByAmount, many)
SELECT id FROM probes WHERE amount = {amount:Int64} ORDER BY id;

-- sgl.query(FindByRatio, many)
SELECT id FROM probes WHERE ratio = {ratio:Float64} ORDER BY id;

-- sgl.query(FindByFlag, many)
SELECT id FROM probes WHERE flag = {flag:Bool} ORDER BY id;

-- sgl.query(FindByNote, many)
SELECT id FROM probes WHERE note = {note:String} ORDER BY id;

-- sgl.query(FindByPayload, many)
SELECT id FROM probes WHERE payload = {payload:String} ORDER BY id;

-- sgl.query(FindByOptionalLabel, many)
SELECT id FROM probes WHERE optional_label = {optional_label:Nullable(String)} ORDER BY id;

-- sgl.query(FindByTags, many)
SELECT id FROM probes WHERE tags = {tags:Array(String)} ORDER BY id;

-- sgl.query(FindByHost, many)
SELECT id FROM probes WHERE host = {host:IPv4} ORDER BY id;

-- sgl.query(FindByOptionalHost, many)
SELECT id FROM probes WHERE optional_host = {optional_host:Nullable(IPv4)} ORDER BY id;

-- sgl.query(FindByAddress, many)
SELECT id FROM probes WHERE address = {address:IPv4} ORDER BY id;

-- sgl.query(ReadLabel, one)
SELECT label FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadAmount, one)
SELECT amount FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadRatio, one)
SELECT ratio FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadFlag, one)
SELECT flag FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadNote, one)
SELECT note FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadPayload, one)
SELECT payload FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadOptionalLabel, one)
SELECT optional_label FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadOptionalAmount, one)
SELECT optional_amount FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadTags, one)
SELECT tags FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadHost, one)
SELECT host FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadOptionalHost, one)
SELECT optional_host FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadAddress, one)
SELECT address FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadPair, one)
SELECT pair FROM probes WHERE id = {id:Int32};

-- sgl.query(ReadKeyed, one)
SELECT keyed FROM probes WHERE id = {id:Int32};

-- sgl.query(ListProbes, many)
SELECT id, label, amount, ratio, flag, note, payload, optional_label, optional_amount, tags, host, optional_host, address, pair, keyed FROM probes ORDER BY id;

-- sgl.query(ListPlainProbes, many)
SELECT id, label, amount, ratio, flag, note, payload, optional_label, optional_amount, tags, address, pair, keyed FROM probes ORDER BY id;
