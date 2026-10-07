-- sgl.query(name: StationMaps, command: many)
SELECT
    id,
    MAP {name: id} AS by_name,
    MAP {'reading': reading, 'twice': reading * 2} AS readings_by_label,
    MAP {1: 2.5, 2: 3} AS widened,
    MAP {name: region} AS regions,
    map([name], [id]) AS zipped,
    map(names, readings) AS zipped_lists,
    map_from_entries([{'k': name, 'v': id}]) AS from_entries,
    map_from_entries([(name, reading)]) AS from_rows,
    MAP {name: {'id': id, 'name': name}} AS named_structs,
    map() AS empty_map
FROM stations;

-- sgl.query(name: StationRows, command: many)
SELECT
    MAP {name: ROW(id, name)} AS literal_rows,
    map([name], [ROW(id, name)]) AS zipped_rows,
    map_from_entries([(name, ROW(id, name))]) AS entry_rows
FROM stations;
