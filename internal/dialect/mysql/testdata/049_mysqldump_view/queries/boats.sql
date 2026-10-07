-- sgl.query(name: ListLongBoats, command: many)
SELECT id, name FROM long_boats;

-- sgl.query(name: ListBoatLabels, command: many)
SELECT boat_label(name) AS label FROM boat_names;
