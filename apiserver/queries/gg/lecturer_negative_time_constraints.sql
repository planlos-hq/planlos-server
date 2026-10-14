select
    id,
    lecturer,
    weekday,
    at::text,
    duration::text,
    soft
from
    lecturer_negative_time_constraint
order by
    lecturer,
    weekday,
    at
