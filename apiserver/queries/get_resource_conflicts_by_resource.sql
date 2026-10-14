select
    resource_type,
    resource_id,
    weekday,
    slot_a_id,
    slot_a_course,
    slot_a_at::text,
    slot_a_duration::text,
    slot_b_id,
    slot_b_course,
    slot_b_at::text,
    slot_b_duration::text
from
    resource_conflicts
where
    resource_type = $1
    and resource_id = $2
order by
    weekday
