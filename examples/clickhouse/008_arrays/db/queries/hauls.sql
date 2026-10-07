-- sgl.query(LandHaul, exec)
INSERT INTO hauls (id, boat, species, weights_kg)
VALUES ({id:UInt64}, {boat:String}, {species:Array(String)}, {weights_kg:Array(UInt32)});

-- sgl.query(GetHaul, one)
SELECT id, boat, species, weights_kg FROM hauls WHERE id = {id:UInt64};

-- sgl.query(HaulsWith, many)
SELECT id, boat, species, weights_kg
FROM hauls
WHERE has(species, {fish:String})
ORDER BY id;
