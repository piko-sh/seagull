-- sgl.query(AddPlace, one)
INSERT INTO places (name, description, tags, west, east, south, north)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id, tag_count;

-- sgl.query(SearchDescriptions, many)
SELECT p.name, snippet(places_search, 1, '[', ']', '...', 6) AS excerpt
FROM places_search
JOIN places AS p ON p.id = places_search.rowid
WHERE places_search.description MATCH :terms
ORDER BY rank;

-- sgl.query(PlacesAt, many)
SELECT p.name
FROM places_area AS area
JOIN places AS p ON p.id = area.id
WHERE area.min_x <= :x AND area.max_x >= :x AND area.min_y <= :y AND area.max_y >= :y
ORDER BY p.name;

-- sgl.query(PlacesTagged, many)
-- :tag as sgl.param(tag, nullable: false)
SELECT p.name, p.tag_count
FROM places AS p, json_each(p.tags) AS tag
WHERE tag.value = :tag
ORDER BY p.name;
