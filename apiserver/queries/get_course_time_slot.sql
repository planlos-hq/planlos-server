select
    id,
    course,
    weekday,
    at::text,
    duration::text,
    one_off_date::text
from
    course_time_slots
where
    id = $1
