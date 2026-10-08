select customer_id, region, cast(customer_id as varchar) as customer_code,
       timestamp '2020-01-01' as valid_from, timestamp '2027-01-01' as valid_to
from (values (1, 'APAC'), (2, 'EMEA')) as customers(customer_id, region)
