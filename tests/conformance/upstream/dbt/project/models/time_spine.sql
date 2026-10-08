select cast(date_day as date) as date_day
from generate_series(date '2026-01-25', date '2026-02-05', interval 1 day) as spine(date_day)
