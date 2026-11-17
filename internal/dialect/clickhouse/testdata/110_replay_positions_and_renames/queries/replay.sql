-- sgl.query(name: ListPlaced, command: many)
SELECT * FROM placed;

-- sgl.query(name: ListRenamed, command: many)
SELECT * FROM new_view;

-- sgl.query(name: ListMoved, command: many)
SELECT id FROM archive.moved;
