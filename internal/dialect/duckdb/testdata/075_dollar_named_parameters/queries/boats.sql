-- sgl.query(BoatsLongerThan, many)
SELECT id, name, $tag$ $length $tag$ AS label FROM boats WHERE length > $min_length AND name <> $name OR length = $min_length;

-- sgl.query(RenameBoat, exec)
UPDATE boats SET name = $new_name WHERE id = :id;
