-- sgl.query(name: Nearest, command: many)
SELECT id, embedding <-> $1::vector AS distance, embedding <=> $1::vector AS cosine, embedding <#> $1::vector AS inner_product
FROM items ORDER BY embedding <-> $1::vector LIMIT $2::int;

-- sgl.query(name: Labelled, command: many)
SELECT id, label FROM items UNION ALL VALUES (0, 'none');
