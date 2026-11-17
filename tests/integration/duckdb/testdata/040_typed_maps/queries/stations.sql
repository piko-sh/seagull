-- sgl.query(name: InsertStation, command: exec)
INSERT INTO stations (id, name, region, reading, names, readings) VALUES ($1, $2, $3, $4, $5, $6);

-- sgl.query(name: StationMaps, command: many)
SELECT
    id,
    MAP {name: id} AS by_name,
    MAP {'reading': reading, 'twice': reading * 2} AS readings_by_label,
    MAP {name: region} AS regions,
    map([name], [id]) AS zipped,
    map(names, readings) AS zipped_lists,
    map_from_entries([{'k': name, 'v': id}]) AS from_entries,
    map_from_entries([(name, reading)]) AS from_rows,
    MAP {name: {'id': id, 'name': name}} AS named_structs,
    histogram(id) OVER () AS id_histogram
FROM stations
ORDER BY id;
