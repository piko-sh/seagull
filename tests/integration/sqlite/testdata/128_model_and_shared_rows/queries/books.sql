-- sgl.query(AddBook, exec)
INSERT INTO books (id, title, pages, published) VALUES (?, ?, ?, ?);

-- sgl.query(GetBook, one, model: books)
SELECT published, pages, title, id FROM books WHERE id = ?;

-- sgl.query(ListBooks, many)
SELECT id, title, pages, published FROM books ORDER BY id;

-- sgl.query(StreamBooks, stream, model: books)
SELECT id, title, pages, published FROM books ORDER BY id;

-- sgl.query(name: SearchBooks, command: many, dynamic: runtime, model: books)
SELECT id, title, pages, published FROM books;

-- sgl.query(ListTitles, many, row: BookTitle)
SELECT id, title FROM books ORDER BY id;

-- sgl.query(TitleOf, one, row: BookTitle)
SELECT title, id FROM books WHERE id = ?;
