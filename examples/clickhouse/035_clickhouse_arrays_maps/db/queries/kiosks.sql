-- sgl.query(OpenKiosk, exec)
INSERT INTO kiosks (id, name, town, till, prices, tags, location)
VALUES (
    {id:UInt64},
    {name:String},
    {town:String},
    {till:IPv4},
    {prices:Map(String, UInt32)},
    {tags:Array(String)},
    ({lat:Float64}, {lon:Float64})
);

-- sgl.query(GetKiosk, one)
SELECT id, name, town, till, prices, tags, location FROM kiosks WHERE id = {id:UInt64};

-- sgl.query(KiosksByTag, many)
SELECT tag, name
FROM kiosks
ARRAY JOIN tags AS tag
ORDER BY tag, name;

-- sgl.query(KiosksInTown, many)
SELECT id, name
FROM kiosks
WHERE town = {town:String}
ORDER BY id;
