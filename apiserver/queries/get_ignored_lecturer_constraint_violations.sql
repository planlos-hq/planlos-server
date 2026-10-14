select
    constraint_id,
    time_slot_id
from
    ignored_lecturer_constraint_violation
order by
    constraint_id,
    time_slot_id
