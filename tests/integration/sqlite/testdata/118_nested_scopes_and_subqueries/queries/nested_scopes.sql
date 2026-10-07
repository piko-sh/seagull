-- sgl.query(name: AuthorTitles, command: many)
WITH author_books AS (
    SELECT d.author_id, d.title FROM (SELECT author_id, title FROM books) d
)
SELECT author_id, count(*) AS book_count FROM author_books GROUP BY author_id ORDER BY author_id;

-- sgl.query(name: NestedWith, command: many)
WITH outer_rows AS (
    WITH inner_rows AS (SELECT id, name FROM authors)
    SELECT id, name FROM inner_rows
)
SELECT id, name FROM outer_rows ORDER BY id;

-- sgl.query(name: StarOfAnEarlierCTE, command: many)
WITH first_rows AS (SELECT id, name FROM authors), second_rows AS (SELECT * FROM first_rows)
SELECT * FROM second_rows ORDER BY id;

-- sgl.query(name: JoinedEarlierCTE, command: many)
WITH named AS (SELECT id, name FROM authors), titled AS (SELECT b.title, n.name FROM books b JOIN named n ON n.id = b.author_id)
SELECT title, name FROM titled ORDER BY title;

-- sgl.query(name: CTEInsideADerivedUnion, command: many)
WITH a AS (SELECT id, name FROM authors)
SELECT u.id, u.name FROM (SELECT id, name FROM a UNION ALL SELECT id, name FROM a) u ORDER BY u.id;

-- sgl.query(name: DerivedTableInALaterArm, command: many)
SELECT id, name FROM authors UNION ALL SELECT d.id, d.title FROM (SELECT id, title FROM books) d ORDER BY 2;
