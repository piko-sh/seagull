-- sgl.query(name: ListVoyages, command: many)
SELECT * FROM voyages ORDER BY id;

-- sgl.query(name: VoyageStops, command: many)
SELECT id, stops FROM voyages WHERE id = 1
UNION ALL
SELECT id, stops FROM voyages WHERE id = 2;

-- sgl.query(name: ArrivalSummary, command: one)
SELECT
    arg_max(arrived, id, 2) AS latest,
    quantile(arrived, [0.5, 1.0]) AS quartiles,
    histogram(day) AS per_day
FROM arrivals;

-- sgl.query(name: ArrivalMaps, command: many)
SELECT id, MAP {day: arrived} AS arrived_by_day, MAP {arrived: id} AS id_by_arrival FROM arrivals ORDER BY id;
