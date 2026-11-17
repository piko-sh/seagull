-- sgl.query(OpenKiosk, exec)
INSERT INTO kiosks (id, location, stock, shelves)
VALUES ({id:UInt64}, {location:Tuple(lat Float64, lon Float64)}, {stock:Map(String, UInt32)}, {shelves:Array(Map(String, UInt32))});

-- sgl.query(KioskAt, many)
SELECT id FROM kiosks WHERE location = {location:Tuple(lat Float64, lon Float64)} ORDER BY id;

-- sgl.query(KioskStocking, many)
SELECT id FROM kiosks WHERE stock = {stock:Map(String, UInt32)} ORDER BY id;

-- sgl.query(KioskShelves, many)
SELECT id FROM kiosks WHERE shelves = {shelves:Array(Map(String, UInt32))} ORDER BY id;
