-- sgl.query(name: ShortRow, command: exec)
INSERT INTO authors (id, name) VALUES ($1);

-- sgl.query(name: NarrowSelect, command: exec)
INSERT INTO authors (id, name) SELECT id FROM books;

-- sgl.query(name: SessionUser, command: exec)
SET LOCAL app.user_id = $1;

-- sgl.query(name: Announce, command: exec)
NOTIFY changes, $1;

-- sgl.query(name: EmbedUnread, command: many)
-- sgl.embed(books)
SELECT id, name FROM authors;
