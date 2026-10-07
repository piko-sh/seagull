-- sgl.query(name: SortedLists, command: many)
SELECT
    list_sort(tags) AS sorted,
    array_sort(tags, 'DESC') AS descending,
    list_sort(scores, 'ASC', 'NULLS LAST') AS ascending,
    list_reverse_sort(scores) AS reversed,
    array_reverse_sort(tags, 'NULLS FIRST') AS reversed_tags,
    list_distinct(tags) AS distinct_tags
FROM boats;
