-- sgl.query(KeyedByLists, many)
SELECT by_crew, MAP {[id]: name} AS by_list
FROM boats;

-- sgl.query(KeyedByStructs, many)
SELECT [by_berth] AS listed, {'inner': MAP {{'id': id}: name}} AS nested
FROM boats;

-- sgl.query(StarredKeys, many)
SELECT * FROM boats;

-- sgl.query(ReadableKeys, many)
SELECT by_day, MAP {name: id} AS by_name, map_entries(by_crew) AS crew_entries, map_keys(by_berth) AS berths
FROM boats;
