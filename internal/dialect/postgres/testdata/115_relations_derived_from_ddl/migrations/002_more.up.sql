SET search_path = 'billing';
CREATE TABLE payments (id BIGINT PRIMARY KEY, invoice_id BIGINT NOT NULL);
SET LOCAL search_path TO public;
CREATE TABLE notices (id BIGINT PRIMARY KEY);
