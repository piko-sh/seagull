-- sgl.query(name: AddNote, command: exec)
INSERT INTO notes (body, kind) VALUES ($1, $2);

-- sgl.query(name: ListNotes, command: many)
SELECT * FROM notes ORDER BY body;

-- sgl.query(name: ListKeyedNotes, command: many)
SELECT rowid > 0 AS keyed, body FROM notes ORDER BY body;

-- sgl.query(name: AddMark, command: exec)
INSERT INTO marks (rowid, label) VALUES ($1, $2);

-- sgl.query(name: ListMarks, command: many)
SELECT rowid_1 > 0 AS keyed, rowid, label FROM marks ORDER BY label;

-- sgl.query(name: AddTag, command: exec)
INSERT INTO tags (label, colour) VALUES ($1, $2);

-- sgl.query(name: ListTags, command: many)
SELECT * FROM tags ORDER BY label;

-- sgl.query(name: ListLabels, command: many)
SELECT * FROM labels ORDER BY name;

-- sgl.query(name: NextNoteNumber, command: one)
SELECT nextval('note_numbers') AS number;

-- sgl.query(name: NoteNumbers, command: one)
SELECT last_value, log_cnt, is_called FROM note_numbers;
