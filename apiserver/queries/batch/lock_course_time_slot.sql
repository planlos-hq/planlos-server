select
    id,
    course,
    weekday,
    at::text,
    duration::text,
    one_off_date::text,
    ($2::int is null
        or course = $2)
    and ($3::smallint is null
        or weekday = $3)
    and ($4::time is null
        or at = $4::time)
    and ($5::interval is null
        or duration = $5::interval) as matches
from
    course_time_slots
where
    id = $1
for update
