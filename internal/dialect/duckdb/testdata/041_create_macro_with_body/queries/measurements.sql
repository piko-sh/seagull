-- sgl.query(name: GetDoubledMeasurements, command: many)
SELECT id, double_value(value) AS doubled FROM measurements;

-- sgl.query(name: GetLabelledMeasurements, command: many)
SELECT id, add_label(value) AS labelled FROM measurements;
