-- sgl.query(AddNote, exec)
INSERT INTO notes (id, tags, scores, plain, limits)
VALUES ({id:Int32}, {tags:Array(Nullable(String))}, {scores:Array(Nullable(Int32))}, {plain:Array(String)},
  {limits:Map(String, Nullable(Int32))});

-- sgl.query(GetNote, one, model: notes)
SELECT limits, plain, scores, tags, id FROM notes WHERE id = {id:Int32};

-- sgl.query(ListNotes, many)
SELECT id, tags, scores, plain, limits FROM notes ORDER BY id;

-- sgl.query(StoredNotes, many)
SELECT id, toString(tags) AS tags, toString(scores) AS scores, toString(limits) AS limits FROM notes ORDER BY id;
