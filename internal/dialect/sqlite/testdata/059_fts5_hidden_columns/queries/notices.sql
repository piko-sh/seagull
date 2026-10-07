-- sgl.query(name: AllNotices, command: many)
SELECT * FROM notices;

-- sgl.query(name: SearchNotices, command: many)
SELECT rowid, title, rank
FROM notices
WHERE notices MATCH ?
ORDER BY rank;

-- sgl.query(name: NoticeByTitle, command: many)
SELECT rowid, body FROM notices WHERE notices = ?;
