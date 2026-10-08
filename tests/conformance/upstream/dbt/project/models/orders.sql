select * from (values
  (1, 1, cast('2026-01-30' as date), cast(50 as decimal(18,2))),
  (2, 1, cast('2026-01-31' as date), cast(70 as decimal(18,2))),
  (3, 2, cast('2026-02-01' as date), cast(80 as decimal(18,2)))
) as orders(order_id, customer_id, ordered_at, amount)
