-- sgl.query(name: PivotFromData, command: many)
PIVOT landings ON species USING sum(kg) GROUP BY harbour;
