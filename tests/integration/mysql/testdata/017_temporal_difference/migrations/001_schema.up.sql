CREATE TABLE shipments (
    id INT NOT NULL PRIMARY KEY,
    ordered_at DATETIME NOT NULL,
    shipped_at DATETIME(3),
    ordered_on DATE NOT NULL,
    due_on DATE NOT NULL
);
