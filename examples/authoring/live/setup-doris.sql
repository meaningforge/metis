-- Run once in a disposable development warehouse. Existing databases are refused.
CREATE DATABASE metis_authoring_demo;
CREATE TABLE metis_authoring_demo.orders (
  order_time DATETIMEV2(6) NOT NULL,
  region VARCHAR(16) NOT NULL,
  amount DECIMAL(18, 2) NULL,
  internal_note STRING NULL
) DUPLICATE KEY(order_time, region)
DISTRIBUTED BY HASH(region) BUCKETS 1
PROPERTIES ("replication_num" = "1");
INSERT INTO metis_authoring_demo.orders VALUES
  ('2026-01-01 12:00:00', 'APAC', 10.25, 'not a published field'),
  ('2026-01-02 12:00:00', 'APAC', NULL, 'amount not recorded'),
  ('2026-01-03 12:00:00', 'EMEA', 7.50, 'not a published field');
