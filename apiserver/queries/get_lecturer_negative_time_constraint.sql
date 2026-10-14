select
    id,
    lecturer,
    weekday,
    at::text,
    duration::text,
    soft
from
    lecturer_negative_time_constraint
where
    id = $1
