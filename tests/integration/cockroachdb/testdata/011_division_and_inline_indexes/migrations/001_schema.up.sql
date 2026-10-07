CREATE TABLE orders (
    id INT PRIMARY KEY,
    email STRING NOT NULL,
    quantity INT NOT NULL,
    parts INT NOT NULL,
    doc JSONB,
    ordered_on DATE NOT NULL,
    shipped_on DATE,
    INDEX orders_email_idx (email) STORING (quantity),
    UNIQUE INDEX orders_email_key (email),
    INVERTED INDEX orders_doc_idx (doc),
    INDEX (ordered_on DESC) WHERE shipped_on IS NULL,
    FAMILY "primary" (id, email, quantity, parts),
    FAMILY extra (doc, ordered_on, shipped_on)
);
