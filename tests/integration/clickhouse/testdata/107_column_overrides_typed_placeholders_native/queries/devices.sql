-- sgl.query(RecordDevice, exec)
INSERT INTO devices (id, addr, label, previous) VALUES ({id:UInt64}, {addr:IPv4}, {label:String}, {previous:Nullable(IPv4)});

-- sgl.query(CopyDevices, copyfrom)
INSERT INTO devices (id, addr, label, previous) VALUES ({id:UInt64}, {addr:IPv4}, {label:String}, {previous:Nullable(IPv4)});

-- sgl.query(InsertDevices, batch)
INSERT INTO devices (id, addr, label, previous) VALUES ({id:UInt64}, {addr:IPv4}, {label:String}, {previous:Nullable(IPv4)});

-- sgl.query(InsertDeviceRows, batch)
INSERT INTO devices (id, addr, label, previous) VALUES ({id:UInt64} + 0, {addr:IPv4}, {label:String}, {previous:Nullable(IPv4)});

-- sgl.query(FindByAddress, many)
SELECT id, addr, label, previous FROM devices WHERE addr = {addr:IPv4} ORDER BY id;

-- sgl.query(FindByAddresses, many)
SELECT id FROM devices WHERE addr IN ({first:IPv4}, {second:IPv4}) ORDER BY id;

-- sgl.query(FindBetween, many)
SELECT id FROM devices WHERE addr BETWEEN {low:IPv4} AND {high:IPv4} AND {label:String} <> label ORDER BY id;

-- sgl.query(FindByPrevious, many)
SELECT id FROM devices WHERE previous = {previous:Nullable(IPv4)} ORDER BY id;

-- sgl.query(MoveDevice, asyncexec)
ALTER TABLE devices UPDATE addr = {addr:IPv4}, label = {label:String} WHERE id = {id:UInt64};

-- sgl.query(ListDevices, many)
SELECT id, addr, label, previous FROM devices ORDER BY id;
