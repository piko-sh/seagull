-- sgl.query(PackedBoats, many)
SELECT struct_pack(id := id, name := name) AS packed, {'name': name, 'length': length} AS literal,
       {key: id, 'Mixed Case': tags, "quoted": 2.5} AS spelt, {'outer': {'inner': id}} AS nested
FROM boats;

-- sgl.query(BoatFields, many)
SELECT {'name': name, 'length': length}.length AS length, struct_pack(id := id).id AS id,
       struct_extract({'tags': tags}, 'tags') AS tags
FROM boats;
