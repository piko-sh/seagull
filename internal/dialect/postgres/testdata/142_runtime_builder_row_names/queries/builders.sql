-- sgl.query(name: LatestBooks, command: many, dynamic: runtime)
SELECT a.id, b.id, b.title FROM books b JOIN authors a ON a.id = b.author_id ORDER BY b.id DESC LIMIT 10;

-- sgl.query(name: LatestBooksNamed, command: many, dynamic: runtime)
SELECT a.id AS author_id, b.id AS book_id, b.title FROM books b JOIN authors a ON a.id = b.author_id ORDER BY b.id DESC LIMIT 10;

-- sgl.query(name: AllBooks, command: many, dynamic: runtime)
SELECT a.id, b.id, b.title FROM books b JOIN authors a ON a.id = b.author_id ORDER BY b.id DESC;
