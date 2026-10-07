-- sgl.query(name: DotField, command: many)
SELECT name, berth.pontoon AS pontoon, berth.number FROM boats;

-- sgl.query(name: QualifiedField, command: many)
SELECT b.berth.pontoon AS pontoon FROM boats b WHERE b.id = $1;

-- sgl.query(name: SubscriptThenDot, command: many)
SELECT name, route[1].lat AS start_lat FROM boats;

-- sgl.query(name: StructExtract, command: many)
SELECT struct_extract(berth, 'number') AS berth_number, berth['pontoon'] AS pontoon FROM boats;

-- sgl.query(name: MapAccess, command: many)
SELECT element_at(fees, 'July') AS july_fees, fees['July'] AS july FROM boats;

-- sgl.query(name: UnionAndListFunctions, command: many)
SELECT union_tag(contact) AS contact_by, union_extract(contact, 'phone') AS phone, len(crew) AS crew_size, array_length(crew) AS crew_length FROM boats;
