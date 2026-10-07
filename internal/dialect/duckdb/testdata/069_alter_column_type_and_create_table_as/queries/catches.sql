-- sgl.query(name: ListCatches, command: many)
SELECT id, weight, species FROM catches;

-- sgl.query(name: ListHeavyCatches, command: many)
SELECT id, weight, doubled FROM heavy_catches;
