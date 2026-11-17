CREATE SEQUENCE invoice_numbers START WITH 1000;
CREATE SEQUENCE IF NOT EXISTS receipt_numbers;
CREATE OR REPLACE SEQUENCE order_numbers INCREMENT BY 2;
CREATE SEQUENCE scratch_numbers;
DROP SEQUENCE IF EXISTS scratch_numbers, missing_numbers;
