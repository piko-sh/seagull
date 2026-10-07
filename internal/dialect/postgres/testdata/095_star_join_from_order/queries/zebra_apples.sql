-- sgl.query(name: ZebraApples, command: many)
SELECT *
FROM zebras z
JOIN apples a ON a.zebra_id = z.id;
