select
    slot_a,
    slot_b,
    resource_type,
    resource_id
from
    ignored_resource_conflict
order by
    resource_type,
    resource_id,
    slot_a
