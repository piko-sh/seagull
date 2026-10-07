-- sgl.query(BoatRows, many)
SELECT boats, crews, to_json(boats) AS document, row_to_json(boats) AS row_document
FROM boats
LEFT JOIN crews ON crews.boat_id = boats.id;

-- sgl.query(BoatKeys, many)
SELECT {'name': name, 'id': id}['id'] AS key, struct_pack(spelt := name)['spelt'] AS spelt,
       struct_insert(berth, depth := 4)['depth'] AS depth
FROM boats;

-- sgl.query(ExtendedBerths, many)
SELECT struct_insert(berth, depth := 4, crane := true) AS extended, struct_insert({'id': id}, name := name) AS built
FROM boats;
