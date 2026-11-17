-- sgl.query(AddAuthor, exec)
INSERT INTO authors (id, name, bio) VALUES ({id:Int64}, {name:String}, {bio:Nullable(String)});

-- sgl.query(ListAuthors, many, row: AuthorSummary)
SELECT id, name, bio FROM authors ORDER BY id;

-- sgl.query(AuthorNamed, one, row: AuthorSummary)
SELECT bio, name, id FROM authors WHERE name = {name:String};
