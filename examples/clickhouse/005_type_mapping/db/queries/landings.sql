-- sgl.query(RecordLanding, exec)
INSERT INTO landings (id, catch_id, species, boxes, fish, weight_g, length_cm, price, on_ice, landed_at, tag, notes)
VALUES (
    {id:UInt64},
    toUUID({catch_id:String}),
    {species:String},
    {boxes:UInt16},
    {fish:UInt32},
    {weight_g:Int64},
    {length_cm:Float32},
    toDecimal32({price:String}, 2),
    {on_ice:Bool},
    {landed_at:DateTime('UTC')},
    {tag:FixedString(4)},
    {notes:Nullable(String)}
);

-- sgl.query(GetLanding, one)
SELECT id, catch_id, species, boxes, fish, weight_g, length_cm, price, on_ice, landed_at, tag, notes
FROM landings
WHERE id = {id:UInt64};
