select
    constraint_id,
    lecturer_id,
    soft,
    weekday,
    constraint_at::text,
    constraint_duration::text,
    time_slot_id,
    course_id,
    slot_at::text,
    slot_duration::text
from
    lecturer_constraint_violations
where
    lecturer_id = $1
order by
    weekday
