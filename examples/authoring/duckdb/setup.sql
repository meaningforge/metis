-- Run independently with the DuckDB CLI against a fresh disposable file.
-- Existing tables cause failure; this script never drops or overwrites them.
CREATE TABLE main.orders (
  order_time TIMESTAMP NOT NULL,
  region VARCHAR NOT NULL,
  amount DECIMAL(18,2),
  internal_note VARCHAR
);
INSERT INTO main.orders VALUES
  ('2026-01-01 10:00:00', 'APAC', 10.25, 'unpublished'),
  ('2026-01-02 10:00:00', 'APAC', NULL, 'unpublished'),
  ('2026-01-03 10:00:00', 'EMEA', 7.50, 'unpublished');
